package dnszone

import (
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go/service/route53"

	corev1 "k8s.io/api/core/v1"

	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1alpha1"
	awsclient "github.com/openshift/hive/pkg/awsclient"
)

const hiveDNSZoneAWSTag = "hive.openshift.io/dnszone"

// Ensure AWSActuator implements the Actuator interface. This will fail at compile time when false.
var _ Actuator = &AWSActuator{}

// AWSActuator manages getting the desired state, getting the current state and reconciling the two.
type AWSActuator struct {
	// logger is the logger used for this controller
	logger log.FieldLogger

	// awsClient is a utility for making it easy for controllers to interface with AWS
	awsClient awsclient.Client

	// zoneID is the ID of the hosted zone in route53
	zoneID *string

	// currentTags are the list of tags associated with the currentHostedZone
	currentHostedZoneTags []*route53.Tag

	// The DNSZone that represents the desired state.
	dnsZone *hivev1.DNSZone
}

type awsClientBuilderType func(secret *corev1.Secret, region string) (awsclient.Client, error)

// NewAWSActuator creates a new AWSActuator object. A new AWSActuator is expected to be created for each controller sync.
func NewAWSActuator(
	logger log.FieldLogger,
	secret *corev1.Secret,
	dnsZone *hivev1.DNSZone,
	awsClientBuilder awsClientBuilderType,
) (*AWSActuator, error) {
	// This allows for using host profiles for AWS auth.
	var regionName string

	if dnsZone != nil && dnsZone.Spec.AWS != nil {
		regionName = dnsZone.Spec.AWS.Region
	}

	awsClient, err := awsClientBuilder(secret, regionName)
	if err != nil {
		logger.WithError(err).Error("Error creating AWSClient")
		return nil, err
	}

	awsActuator := &AWSActuator{
		logger:    logger,
		awsClient: awsClient,
		dnsZone:   dnsZone,
	}

	return awsActuator, nil
}

// UpdateMetadata ensures that the Route53 hosted zone metadata is current with the DNSZone
func (actuator *AWSActuator) UpdateMetadata() error {
	if actuator.zoneID == nil {
		return errors.New("zoneID is unpopulated")
	}

	// For now, tags are the only things we can sync with existing zones.
	return actuator.syncTags()
}

// syncTags determines if there are changes that need to happen to match tags in the spec
func (actuator *AWSActuator) syncTags() error {
	existingTags := actuator.currentHostedZoneTags
	expected := actuator.expectedTags()
	toAdd := []*route53.Tag{}
	toDelete := make([]*route53.Tag, len(existingTags))
	// Initially add all existing tags to the toDelete array
	// As they're found in the expected array, remove them from
	// the toDelete array
	copy(toDelete, existingTags)

	logger := actuator.logger.WithField("id", actuator.zoneID)
	logger.WithField("current", tagsString(existingTags)).WithField("expected", tagsString(expected)).Debug("syncing tags")

	for _, tag := range expected {
		found := false
		for i, actualTag := range toDelete {
			if tagEquals(tag, actualTag) {
				found = true
				toDelete = append(toDelete[:i], toDelete[i+1:]...)
				logger.WithField("tag", tagString(tag)).Debug("tag already exists, will not be added")
				break
			}
		}
		if !found {
			logger.WithField("tag", tagString(tag)).Debug("tag will be added")
			toAdd = append(toAdd, tag)
		}
	}

	if len(toDelete) == 0 && len(toAdd) == 0 {
		logger.Debug("tags are in sync, no action required")
		return nil
	}

	keysToDelete := make([]*string, 0, len(toDelete))
	for _, tag := range toDelete {
		logger.WithField("tag", tagString(tag)).Debug("tag will be deleted")
		keysToDelete = append(keysToDelete, tag.Key)
	}

	// Only 10 tags can be added/removed at a time. Iterate until all tags are added/removed
	index := 0
	for len(toAdd) > index || len(keysToDelete) > index {
		toAddSegment := []*route53.Tag{}
		keysToDeleteSegment := []*string{}

		if len(toAdd) > index {
			toAddSegment = toAdd[index:min(index+10, len(toAdd))]
		}

		if len(keysToDelete) > index {
			keysToDeleteSegment = keysToDelete[index:min(index+10, len(keysToDelete))]
		}

		if len(toAddSegment) == 0 {
			toAddSegment = nil
		}
		if len(keysToDeleteSegment) == 0 {
			keysToDeleteSegment = nil
		}

		logger.Debugf("Adding %d tags, deleting %d tags", len(toAddSegment), len(keysToDeleteSegment))
		_, err := actuator.awsClient.ChangeTagsForResource(&route53.ChangeTagsForResourceInput{
			AddTags:       toAddSegment,
			RemoveTagKeys: keysToDeleteSegment,
			ResourceId:    actuator.zoneID,
			ResourceType:  aws.String("hostedzone"),
		})
		if err != nil {
			logger.WithError(err).Error("Cannot update tags for hosted zone")
			return err
		}
		index += 10
	}

	return nil
}

// ModifyStatus updates the DnsZone's status with AWS specific information.
func (actuator *AWSActuator) ModifyStatus() error {
	if actuator.zoneID == nil {
		return errors.New("currentHostedZone is unpopulated")
	}

	actuator.dnsZone.Status.AWS = &hivev1.AWSDNSZoneStatus{
		ZoneID: actuator.zoneID,
	}

	return nil
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

// Refresh gets the AWS object for the zone.
// If a zone cannot be found or no longer exists, actuator.currentHostedZone remains unset.
func (actuator *AWSActuator) Refresh() error {
	var zoneID string
	var err error
	if actuator.dnsZone.Status.AWS != nil && actuator.dnsZone.Status.AWS.ZoneID != nil {
		actuator.logger.Debug("Zone ID is set in status, will retrieve by ID")
		zoneID = *actuator.dnsZone.Status.AWS.ZoneID
	}
	if len(zoneID) == 0 {
		actuator.logger.Debug("Zone ID is not set in status, looking up by tag")
		zoneID, err = actuator.findZoneIDByTag()
		if err != nil {
			actuator.logger.WithError(err).Error("Failed to lookup zone by tag")
			return err
		}
	}
	if len(zoneID) == 0 {
		actuator.logger.Debug("No matching existing zone found")
		return nil
	}

	// Fetch the hosted zone
	logger := actuator.logger.WithField("id", zoneID)
	logger.Debug("Fetching hosted zone by ID")
	resp, err := actuator.awsClient.GetHostedZone(&route53.GetHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == route53.ErrCodeNoSuchHostedZone {
				logger.Debug("Zone no longer exists")
				return nil
			}
		}
		logger.WithError(err).Error("Cannot get hosted zone")
		return err
	}
	logger.Debug("Found hosted zone")

	logger.Debug("Fetching hosted zone tags")
	tags, err := actuator.existingTags(resp.HostedZone.Id)
	if err != nil {
		logger.WithError(err).Error("Cannot get hosted zone tags")
		return err
	}

	actuator.zoneID = resp.HostedZone.Id
	actuator.currentHostedZoneTags = tags

	return nil
}

func (actuator *AWSActuator) findZoneIDByTag() (string, error) {
	tagFilter := &resourcegroupstaggingapi.TagFilter{
		Key:    aws.String(hiveDNSZoneAWSTag),
		Values: []*string{aws.String(fmt.Sprintf("%s/%s", actuator.dnsZone.Namespace, actuator.dnsZone.Name))},
	}
	filterString := fmt.Sprintf("%s=%s", aws.StringValue(tagFilter.Key), aws.StringValue(tagFilter.Values[0]))
	actuator.logger.WithField("filter", filterString).Debug("Searching for zone by tag")
	id := ""
	err := actuator.awsClient.GetResourcesPages(&resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: []*string{aws.String("route53:hostedzone")},
		TagFilters:          []*resourcegroupstaggingapi.TagFilter{tagFilter},
	}, func(resp *resourcegroupstaggingapi.GetResourcesOutput, lastPage bool) bool {
		for _, zone := range resp.ResourceTagMappingList {
			logger := actuator.logger.WithField("arn", aws.StringValue(zone.ResourceARN))
			logger.Debug("Processing search result")
			zoneARN, err := arn.Parse(aws.StringValue(zone.ResourceARN))
			if err != nil {
				logger.WithError(err).Error("Failed to parse hostedzone ARN")
				continue
			}
			elems := strings.Split(zoneARN.Resource, "/")
			if len(elems) != 2 || elems[0] != "hostedzone" {
				logger.Error("Unexpected hostedzone ARN")
				continue
			}
			id = elems[1]
			logger.WithField("id", id).Debug("Found hosted zone")
			return false
		}
		return true
	})
	return id, err
}

func (actuator *AWSActuator) expectedTags() []*route53.Tag {
	tags := []*route53.Tag{
		{
			Key:   aws.String(hiveDNSZoneAWSTag),
			Value: aws.String(fmt.Sprintf("%s/%s", actuator.dnsZone.Namespace, actuator.dnsZone.Name)),
		},
	}
	if actuator.dnsZone.Spec.AWS != nil {
		for _, tag := range actuator.dnsZone.Spec.AWS.AdditionalTags {
			tags = append(tags, &route53.Tag{
				Key:   aws.String(tag.Key),
				Value: aws.String(tag.Value),
			})
		}
	}
	actuator.logger.WithField("tags", tagsString(tags)).Debug("Expected tags")
	return tags
}

func (actuator *AWSActuator) existingTags(zoneID *string) ([]*route53.Tag, error) {
	logger := actuator.logger.WithField("zone", aws.StringValue(zoneID))
	logger.Debug("listing existing tags for zone")
	resp, err := actuator.awsClient.ListTagsForResource(&route53.ListTagsForResourceInput{
		ResourceId:   zoneID,
		ResourceType: aws.String("hostedzone"),
	})
	if err != nil {
		logger.WithError(err).Error("cannot list tags for zone")
		return nil, err
	}
	logger.WithField("tags", tagsString(resp.ResourceTagSet.Tags)).Debug("retrieved zone tags")
	return resp.ResourceTagSet.Tags, nil
}

// Create makes an AWS Route53 hosted zone given the DNSZone object.
func (actuator *AWSActuator) Create() error {
	logger := actuator.logger.WithField("zone", actuator.dnsZone.Spec.Zone)
	logger.Info("Creating route53 hostedzone")
	var hostedZone *route53.HostedZone
	resp, err := actuator.awsClient.CreateHostedZone(&route53.CreateHostedZoneInput{
		Name: aws.String(actuator.dnsZone.Spec.Zone),
		// We use the UID of the HostedZone resource as the caller reference so that if
		// we fail to update the status of the HostedZone with the ID of the recently
		// created zone, we don't attempt to recreate it. Same if communication fails on
		// the response from AWS.
		CallerReference: aws.String(string(actuator.dnsZone.UID)),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == route53.ErrCodeHostedZoneAlreadyExists {
			// If the zone was already created, we need to find its ID
			logger.WithField("callerRef", actuator.dnsZone.UID).Debug("Hosted zone already exists, looking up by caller reference")
			hostedZone, err = actuator.findZoneByCallerReference(actuator.dnsZone.Spec.Zone, string(actuator.dnsZone.UID))
			if err != nil {
				logger.Error("Failed to find zone by caller reference")
				return err
			}
		} else {
			logger.WithError(err).Error("Error creating hosted zone")
			return err
		}
	} else {
		logger.Debug("Hosted zone successfully created")
		hostedZone = resp.HostedZone
	}

	logger = logger.WithField("id", aws.StringValue(hostedZone.Id))
	logger.Debug("Fetching zone tags")
	existingTags, err := actuator.existingTags(hostedZone.Id)
	if err != nil {
		logger.WithError(err).Error("Failed to fetch zone tags")
		return err
	}

	actuator.zoneID = hostedZone.Id
	actuator.currentHostedZoneTags = existingTags

	logger.Debug("Syncing zone tags")
	err = actuator.syncTags()
	if err != nil {
		// When an error occurs tagging the resource, we return an error. This will result in a retry of the create call.
		// Because we're using the DNSZone's UID as the CallerReference, the create should succeed without creating a duplicate
		// zone. We will then retry adding the tags.
		logger.WithError(err).Error("Failed to apply tags to newly created zone")
		return err
	}

	return err
}

func (actuator *AWSActuator) findZoneByCallerReference(domain, callerRef string) (*route53.HostedZone, error) {
	logger := actuator.logger.WithField("domain", domain).WithField("callerRef", callerRef)
	logger.Debug("Searching for zone by domain and callerRef")
	var nextZoneID *string
	var nextName = aws.String(domain)
	for {
		logger.Debug("listing hosted zones by name")
		resp, err := actuator.awsClient.ListHostedZonesByName(&route53.ListHostedZonesByNameInput{
			DNSName:      nextName,
			HostedZoneId: nextZoneID,
			MaxItems:     aws.String("50"),
		})
		if err != nil {
			logger.WithError(err).Error("cannot list zones by name")
			return nil, err
		}
		for _, zone := range resp.HostedZones {
			if aws.StringValue(zone.CallerReference) == callerRef {
				logger.WithField("zone", aws.StringValue(zone.Id)).Debug("found hosted zone matching caller reference")
				return zone, nil
			}
			if aws.StringValue(zone.Name) != domain {
				logger.WithField("name", aws.StringValue(zone.Name)).Debug("reached zone with different domain name, aborting search")
				return nil, fmt.Errorf("Hosted zone not found")
			}
		}
		if !aws.BoolValue(resp.IsTruncated) {
			logger.Debug("reached end of results, did not find hosted zone")
			return nil, fmt.Errorf("Hosted zone not found")
		}
		nextZoneID = resp.NextHostedZoneId
		nextName = resp.NextDNSName
	}
}

// Delete removes an AWS Route53 hosted zone, typically because the DNSZone object is in a deleting state.
func (actuator *AWSActuator) Delete() error {
	if actuator.zoneID == nil {
		return errors.New("currentHostedZone is unpopulated")
	}

	logger := actuator.logger.WithField("zone", actuator.dnsZone.Spec.Zone).WithField("id", aws.StringValue(actuator.zoneID))
	logger.Info("Deleting route53 hostedzone")
	_, err := actuator.awsClient.DeleteHostedZone(&route53.DeleteHostedZoneInput{
		Id: actuator.zoneID,
	})
	if err != nil {
		log.WithError(err).Error("Cannot delete hosted zone")
	}
	return err
}

// GetNameServers returns the nameservers listed in the route53 hosted zone NS record.
func (actuator *AWSActuator) GetNameServers() ([]string, error) {
	if actuator.zoneID == nil {
		return nil, errors.New("currentHostedZone is unpopulated")
	}

	logger := actuator.logger.WithField("id", actuator.zoneID)
	logger.Debug("Listing hosted zone NS records")
	resp, err := actuator.awsClient.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(*actuator.zoneID),
		StartRecordType: aws.String("NS"),
		StartRecordName: aws.String(actuator.dnsZone.Spec.Zone),
		MaxItems:        aws.String("1"),
	})
	if err != nil {
		logger.WithError(err).Error("Error listing recordsets for zone")
		return nil, err
	}
	if len(resp.ResourceRecordSets) != 1 {
		msg := fmt.Sprintf("unexpected number of recordsets returned: %d", len(resp.ResourceRecordSets))
		logger.Error(msg)
		return nil, fmt.Errorf(msg)
	}
	if aws.StringValue(resp.ResourceRecordSets[0].Type) != "NS" {
		msg := "name server record not found"
		logger.Error(msg)
		return nil, fmt.Errorf(msg)
	}
	if aws.StringValue(resp.ResourceRecordSets[0].Name) != (actuator.dnsZone.Spec.Zone + ".") {
		msg := fmt.Sprintf("name server record not found for domain %s", actuator.dnsZone.Spec.Zone)
		logger.Error(msg)
		return nil, fmt.Errorf(msg)
	}
	result := make([]string, len(resp.ResourceRecordSets[0].ResourceRecords))
	for i, record := range resp.ResourceRecordSets[0].ResourceRecords {
		result[i] = aws.StringValue(record.Value)
	}
	logger.WithField("nameservers", strings.Join(result, ",")).Debug("found hosted zone name servers")
	return result, nil
}

// Exists determines if the route53 hosted zone corresponding to the DNSZone exists
func (actuator *AWSActuator) Exists() (bool, error) {
	return actuator.zoneID != nil, nil
}

func tagEquals(a, b *route53.Tag) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return aws.StringValue(a.Key) == aws.StringValue(b.Key) &&
		aws.StringValue(a.Value) == aws.StringValue(b.Value)
}

func tagString(tag *route53.Tag) string {
	return fmt.Sprintf("%s=%s", aws.StringValue(tag.Key), aws.StringValue(tag.Value))
}

func tagsString(tags []*route53.Tag) string {
	return strings.Join(func() []string {
		result := []string{}
		for _, tag := range tags {
			result = append(result, tagString(tag))
		}
		return result
	}(), ",")
}
