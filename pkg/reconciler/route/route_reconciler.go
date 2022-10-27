package route

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/client-go/tools/cache"

	"github.com/kuadrant/kcp-glbc/pkg/migration/workload"
	"github.com/kuadrant/kcp-glbc/pkg/traffic"

	utilserrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/kuadrant/kcp-glbc/pkg/_internal/metadata"
)

func (c *Controller) reconcile(ctx context.Context, route *traffic.Route) error {
	if route.DeletionTimestamp == nil {
		metadata.AddFinalizer(route, traffic.FINALIZER_CASCADE_CLEANUP)
	}
	// TODO evaluate where this actually belongs
	workload.Migrate(route, c.Queue, c.Logger)

	reconcilers := []traffic.Reconciler{
		//hostReconciler is first as the others depends on it for the host to be set on the route
		&traffic.HostReconciler{
			Log:                    c.Logger,
			CustomHostsEnabled:     c.customHostsEnabled,
			GetDomainVerifications: c.getDomainVerifications,
			CreateOrUpdateTraffic:  c.createOrUpdateRoute,
			DeleteTraffic:          c.deleteRoute,
			GetDNS:                 c.getDNS,
		},
		&traffic.CertificateReconciler{
			Log:                  c.Logger,
			CreateCertificate:    c.certProvider.Create,
			DeleteCertificate:    c.certProvider.Delete,
			GetCertificateSecret: c.certProvider.GetCertificateSecret,
			UpdateCertificate:    c.certProvider.Update,
			GetCertificateStatus: c.certProvider.GetCertificateStatus,
			CopySecret:           c.copySecret,
			DeleteSecret:         c.deleteTLSSecret,
			GetDNS:               c.getDNS,
			GetSecret:            c.getSecret,
		},
		&traffic.DnsReconciler{
			DeleteDNS:        c.deleteDNS,
			DNSLookup:        c.hostResolver.LookupIPAddr,
			GetDNS:           c.getDNS,
			CreateDNS:        c.createDNS,
			UpdateDNS:        c.updateDNS,
			WatchHost:        c.hostsWatcher.StartWatching,
			ForgetHost:       c.hostsWatcher.StopWatching,
			ListHostWatchers: c.hostsWatcher.ListHostRecordWatchers,
			ManagedDomain:    c.domain,
			Log:              c.Logger,
		},
	}
	var errs []error

	for _, r := range reconcilers {
		status, err := r.Reconcile(ctx, route)
		if err != nil {
			errs = append(errs, fmt.Errorf("error from reconciler %v, error: %v", r.GetName(), err))
		}
		if status == traffic.ReconcileStatusStop {
			break
		}
	}

	if len(errs) == 0 {
		if route.DeletionTimestamp != nil && !route.DeletionTimestamp.IsZero() {
			metadata.RemoveFinalizer(route, traffic.FINALIZER_CASCADE_CLEANUP)
			c.hostsWatcher.StopWatching(routeKey(route), "")
			//in 0.5.0 these are never cleaned up properly
			for _, f := range route.Finalizers {
				if strings.Contains(f, workload.SyncerFinalizer) {
					metadata.RemoveFinalizer(route, f)
				}
			}
		}
	} else {
		c.Logger.V(3).Info("route reconcile completed with errors", "reconciler errors", strconv.Itoa(len(errs)), "namespace", route.Namespace, "resource name", route.Name)
	}

	return utilserrors.NewAggregate(errs)
}

func routeKey(route *traffic.Route) interface{} {
	key, _ := cache.MetaNamespaceKeyFunc(route)
	return cache.ExplicitKey(key)
}
