// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	versioned "github.com/openshift/hive/pkg/client/clientset/versioned"
	internalinterfaces "github.com/openshift/hive/pkg/client/informers/externalversions/internalinterfaces"
	v1 "github.com/openshift/hive/pkg/client/listers/hive/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ClusterSyncSetInformer provides access to a shared informer and lister for
// ClusterSyncSets.
type ClusterSyncSetInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.ClusterSyncSetLister
}

type clusterSyncSetInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewClusterSyncSetInformer constructs a new informer for ClusterSyncSet type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewClusterSyncSetInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredClusterSyncSetInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredClusterSyncSetInformer constructs a new informer for ClusterSyncSet type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredClusterSyncSetInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.HiveV1().ClusterSyncSets(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.HiveV1().ClusterSyncSets(namespace).Watch(context.TODO(), options)
			},
		},
		&hivev1.ClusterSyncSet{},
		resyncPeriod,
		indexers,
	)
}

func (f *clusterSyncSetInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredClusterSyncSetInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *clusterSyncSetInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&hivev1.ClusterSyncSet{}, f.defaultInformer)
}

func (f *clusterSyncSetInformer) Lister() v1.ClusterSyncSetLister {
	return v1.NewClusterSyncSetLister(f.Informer().GetIndexer())
}
