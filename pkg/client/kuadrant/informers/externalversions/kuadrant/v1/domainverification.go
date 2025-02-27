// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	versioned "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	internalinterfaces "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions/internalinterfaces"
	v1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/listers/kuadrant/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// DomainVerificationInformer provides access to a shared informer and lister for
// DomainVerifications.
type DomainVerificationInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.DomainVerificationLister
}

type domainVerificationInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewDomainVerificationInformer constructs a new informer for DomainVerification type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewDomainVerificationInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredDomainVerificationInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredDomainVerificationInformer constructs a new informer for DomainVerification type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredDomainVerificationInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return NewFilteredDomainVerificationInformerWithOptions(client, tweakListOptions, cache.WithResyncPeriod(resyncPeriod), cache.WithIndexers(indexers))
}

func NewFilteredDomainVerificationInformerWithOptions(client versioned.Interface, tweakListOptions internalinterfaces.TweakListOptionsFunc, opts ...cache.SharedInformerOption) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformerWithOptions(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KuadrantV1().DomainVerifications().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KuadrantV1().DomainVerifications().Watch(context.TODO(), options)
			},
		},
		&kuadrantv1.DomainVerification{},
		opts...,
	)
}

func (f *domainVerificationInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	indexers := cache.Indexers{}
	for k, v := range f.factory.ExtraClusterScopedIndexers() {
		indexers[k] = v
	}

	return NewFilteredDomainVerificationInformerWithOptions(client,
		f.tweakListOptions,
		cache.WithResyncPeriod(resyncPeriod),
		cache.WithIndexers(indexers),
		cache.WithKeyFunction(f.factory.KeyFunction()),
	)
}

func (f *domainVerificationInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&kuadrantv1.DomainVerification{}, f.defaultInformer)
}

func (f *domainVerificationInformer) Lister() v1.DomainVerificationLister {
	return v1.NewDomainVerificationLister(f.Informer().GetIndexer())
}
