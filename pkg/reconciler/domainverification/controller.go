package domainverification

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kcp-dev/logicalcluster/v2"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	kuadrantv1list "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/listers/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	basereconciler "github.com/kuadrant/kcp-glbc/pkg/reconciler"
)

const (
	defaultControllerName = "kcp-glbc-domain-validation"
	recheckDefault        = time.Second * 5
)

// NewController returns a new Controller which reconciles DomainValidation.
func NewController(config *ControllerConfig) (*Controller, error) {
	controllerName := config.GetName(defaultControllerName)

	dnsVerifier := config.DNSVerifier
	switch impl := dnsVerifier.(type) {
	case *net.ConfigMapHostResolver:
		impl.Client = config.KubeClient
	}

	dnsVerifier = NewSafeDNSVerifier(dnsVerifier)

	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)
	c := &Controller{
		Controller:               basereconciler.NewController(controllerName, queue),
		KubeClient:               config.KubeClient,
		KCPKubeClient:            config.KCPKubeClient,
		domainVerificationClient: config.DomainVerificationClient,
		sharedInformerFactory:    config.SharedInformerFactory,
		dnsVerifier:              dnsVerifier,
	}
	c.Process = c.process

	c.sharedInformerFactory.Kuadrant().V1().DomainVerifications().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.Enqueue(obj) },
		UpdateFunc: func(_, obj interface{}) { c.Enqueue(obj) },
		DeleteFunc: func(obj interface{}) { c.Enqueue(obj) },
	})

	c.indexer = c.sharedInformerFactory.Kuadrant().V1().DomainVerifications().Informer().GetIndexer()
	c.domainVerificationLister = c.sharedInformerFactory.Kuadrant().V1().DomainVerifications().Lister()
	return c, nil

}

type Controller struct {
	*basereconciler.Controller
	indexer                  cache.Indexer
	domainVerificationLister kuadrantv1list.DomainVerificationLister
	domainVerificationClient kuadrantv1.ClusterInterface
	KCPKubeClient            kubernetes.ClusterInterface
	KubeClient               kubernetes.Interface
	sharedInformerFactory    externalversions.SharedInformerFactory
	dnsVerifier              DNSVerifier
}

type ControllerConfig struct {
	*basereconciler.ControllerConfig
	KCPKubeClient            kubernetes.ClusterInterface
	KubeClient               *kubernetes.Clientset
	DomainVerificationClient kuadrantv1.ClusterInterface
	SharedInformerFactory    externalversions.SharedInformerFactory
	DNSVerifier              DNSVerifier
	GLBCWorkspace            logicalcluster.Name
}

func (c *Controller) process(ctx context.Context, key string) error {
	domainVerification, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		c.Logger.Info("DomainVerification was deleted", "key", key)
		return nil
	}

	current := domainVerification.(*v1.DomainVerification)
	previous := current.DeepCopy()

	if err = c.reconcile(ctx, current); err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(previous.Status, current.Status) {
		refresh, err := c.domainVerificationClient.Cluster(logicalcluster.From(current)).KuadrantV1().DomainVerifications().UpdateStatus(ctx, current, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("could not update status: %v", err)
		}
		current.ObjectMeta.ResourceVersion = refresh.ObjectMeta.ResourceVersion
	}

	if !equality.Semantic.DeepEqual(previous, current) {
		_, err := c.domainVerificationClient.Cluster(logicalcluster.From(current)).KuadrantV1().DomainVerifications().Update(ctx, current, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("could not update object: %v", err)
		}
	}

	return nil
}

type SafeDNSVerifier struct {
	DNSVerifier

	mu sync.Mutex
}

func NewSafeDNSVerifier(inner DNSVerifier) *SafeDNSVerifier {
	return &SafeDNSVerifier{
		DNSVerifier: inner,
	}
}

func (r *SafeDNSVerifier) TxtRecordExists(ctx context.Context, domain string, value string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.DNSVerifier.TxtRecordExists(ctx, domain, value)
}
