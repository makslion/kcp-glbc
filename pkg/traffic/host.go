package traffic

import (
	"context"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"github.com/rs/xid"

	"github.com/kuadrant/kcp-glbc/pkg/_internal/metadata"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

type HostReconciler struct {
	Log                    logr.Logger
	CustomHostsEnabled     bool
	GetDomainVerifications func(ctx context.Context, accessor Interface) (*v1.DomainVerificationList, error)
	GetDNS                 func(ctx context.Context, accessor Interface) (*v1.DNSRecord, error)
	CreateOrUpdateTraffic  CreateOrUpdateTraffic
	DeleteTraffic          DeleteTraffic
}

func (r *HostReconciler) GetName() string {
	return "Host Reconciler"
}

func (r *HostReconciler) Reconcile(ctx context.Context, accessor Interface) (ReconcileStatus, error) {
	if !r.CustomHostsEnabled {
		hcgHost, err := accessor.GetHCGhost(ctx, r.GetDNS)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return ReconcileStatusContinue, nil
			}
			return ReconcileStatusStop, err
		}
		if hcgHost == "" {
			return ReconcileStatusStop, nil
		}
		replacedHosts := accessor.ReplaceCustomHosts(hcgHost)
		if len(replacedHosts) > 0 {
			metadata.AddAnnotation(accessor, ANNOTATION_HCG_CUSTOM_HOST_REPLACED, fmt.Sprintf(" replaced custom hosts %v to the glbc host due to custom host policy not being allowed", replacedHosts))
		}
		return ReconcileStatusContinue, nil
	}
	dvs, err := r.GetDomainVerifications(ctx, accessor)
	if err != nil {
		return ReconcileStatusContinue, fmt.Errorf("error getting domain verifications: %v", err)
	}
	err = accessor.ProcessCustomHosts(ctx, dvs, r.CreateOrUpdateTraffic, r.DeleteTraffic, r.GetDNS)
	if err != nil {
		return ReconcileStatusStop, fmt.Errorf("error processing custom hosts: %v", err)
	}
	return ReconcileStatusContinue, nil
}

// AddHostAnnotations adds generated host annotation to a provided DNS Record CR
func AddHostAnnotations(record metav1.Object, host string) {
	if !metadata.HasAnnotation(record, ANNOTATION_HCG_HOST) {
		// Let's assign it a global hostname if any
		generatedHost := fmt.Sprintf("%s.%s", xid.New(), host)
		metadata.AddAnnotation(record, ANNOTATION_HCG_HOST, generatedHost)
		//we need this host set and saved on the accessor before we go any further so force an update
		// if this is not saved we end up with a new host and the certificate can have the wrong host
	}
}
