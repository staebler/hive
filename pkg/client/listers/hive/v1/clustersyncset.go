// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ClusterSyncSetLister helps list ClusterSyncSets.
type ClusterSyncSetLister interface {
	// List lists all ClusterSyncSets in the indexer.
	List(selector labels.Selector) (ret []*v1.ClusterSyncSet, err error)
	// ClusterSyncSets returns an object that can list and get ClusterSyncSets.
	ClusterSyncSets(namespace string) ClusterSyncSetNamespaceLister
	ClusterSyncSetListerExpansion
}

// clusterSyncSetLister implements the ClusterSyncSetLister interface.
type clusterSyncSetLister struct {
	indexer cache.Indexer
}

// NewClusterSyncSetLister returns a new ClusterSyncSetLister.
func NewClusterSyncSetLister(indexer cache.Indexer) ClusterSyncSetLister {
	return &clusterSyncSetLister{indexer: indexer}
}

// List lists all ClusterSyncSets in the indexer.
func (s *clusterSyncSetLister) List(selector labels.Selector) (ret []*v1.ClusterSyncSet, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ClusterSyncSet))
	})
	return ret, err
}

// ClusterSyncSets returns an object that can list and get ClusterSyncSets.
func (s *clusterSyncSetLister) ClusterSyncSets(namespace string) ClusterSyncSetNamespaceLister {
	return clusterSyncSetNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ClusterSyncSetNamespaceLister helps list and get ClusterSyncSets.
type ClusterSyncSetNamespaceLister interface {
	// List lists all ClusterSyncSets in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1.ClusterSyncSet, err error)
	// Get retrieves the ClusterSyncSet from the indexer for a given namespace and name.
	Get(name string) (*v1.ClusterSyncSet, error)
	ClusterSyncSetNamespaceListerExpansion
}

// clusterSyncSetNamespaceLister implements the ClusterSyncSetNamespaceLister
// interface.
type clusterSyncSetNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ClusterSyncSets in the indexer for a given namespace.
func (s clusterSyncSetNamespaceLister) List(selector labels.Selector) (ret []*v1.ClusterSyncSet, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ClusterSyncSet))
	})
	return ret, err
}

// Get retrieves the ClusterSyncSet from the indexer for a given namespace and name.
func (s clusterSyncSetNamespaceLister) Get(name string) (*v1.ClusterSyncSet, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("clustersyncset"), name)
	}
	return obj.(*v1.ClusterSyncSet), nil
}
