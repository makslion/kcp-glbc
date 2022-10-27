package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/strings/slices"

	workload "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
	"github.com/kuadrant/kcp-glbc/pkg/_internal/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/_internal/slice"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/dns"
)

func NewIngress(i *networkingv1.Ingress) *Ingress {
	return &Ingress{Ingress: i}
}

type Ingress struct {
	*networkingv1.Ingress
}

func (a *Ingress) GetKind() string {
	return "Ingress"
}

func (a *Ingress) GetHosts() []string {
	var hosts []string
	for _, rule := range a.Spec.Rules {
		if !slices.Contains(hosts, rule.Host) {
			hosts = append(hosts, rule.Host)
		}
	}

	return hosts
}

func (a *Ingress) GetHCGhost(ctx context.Context, getDNSfunc getDNSrecordFunc) (string, error) {
	record, err := getDNSfunc(ctx, a)
	if err != nil {
		return "", err
	}
	return metadata.GetAnnotation(record, ANNOTATION_HCG_HOST), nil
}

func (a *Ingress) AddTLS(host string, secret *corev1.Secret) {
	for i, tls := range a.Spec.TLS {
		if slice.ContainsString(tls.Hosts, host) {
			a.Spec.TLS[i] = networkingv1.IngressTLS{
				Hosts:      []string{host},
				SecretName: secret.Name,
			}
			return
		}
	}
	a.Spec.TLS = append(a.Spec.TLS, networkingv1.IngressTLS{
		Hosts:      []string{host},
		SecretName: secret.GetName(),
	})
}

func (a *Ingress) RemoveTLS(hosts []string) {
	for _, removeHost := range hosts {
		for i, tls := range a.Spec.TLS {
			hosts := tls.Hosts
			for j, host := range tls.Hosts {
				if host == removeHost {
					hosts = append(hosts[:j], hosts[j+1:]...)
				}
			}
			// if there are no hosts remaining remove the entry for TLS
			if len(hosts) == 0 {
				a.Spec.TLS = append(a.Spec.TLS[:i], a.Spec.TLS[i+1:]...)
			} else {
				a.Spec.TLS[i].Hosts = hosts
			}
		}
	}
}

func (a *Ingress) ReplaceCustomHosts(managedHost string) []string {
	var customHosts []string
	for i, rule := range a.Spec.Rules {
		if rule.Host != managedHost {
			a.Spec.Rules[i].Host = managedHost
			customHosts = append(customHosts, rule.Host)
		}
	}
	// clean up replaced hosts from the tls list
	a.RemoveTLS(customHosts)

	return customHosts
}

func (a *Ingress) GetTargets(ctx context.Context, dnsLookup dnsLookupFunc) (map[logicalcluster.Name]map[string]dns.Target, error) {
	statuses, err := a.getStatuses()
	if err != nil {
		return nil, err
	}

	targets := map[logicalcluster.Name]map[string]dns.Target{}
	for cluster, status := range statuses {
		statusTargets, err := a.targetsFromStatus(ctx, status, dnsLookup)
		if err != nil {
			return nil, err
		}
		targets[cluster] = statusTargets
	}

	return targets, nil
}

func (a *Ingress) targetsFromStatus(ctx context.Context, status networkingv1.IngressStatus, dnsLookup dnsLookupFunc) (map[string]dns.Target, error) {
	targets := map[string]dns.Target{}
	for _, lb := range status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets[lb.IP] = dns.Target{Value: []string{lb.IP}, TargetType: dns.TargetTypeIP}
		}
		if lb.Hostname != "" {
			ips, err := dnsLookup(ctx, lb.Hostname)
			if err != nil {
				return nil, err
			}
			targets[lb.Hostname] = dns.Target{Value: []string{}, TargetType: dns.TargetTypeHost}
			for _, ip := range ips {
				t := targets[lb.Hostname]
				t.Value = append(targets[lb.Hostname].Value, ip.IP.String())
				targets[lb.Hostname] = t
			}
		}
	}
	return targets, nil
}

func (a *Ingress) getStatuses() (map[logicalcluster.Name]networkingv1.IngressStatus, error) {
	statuses := map[logicalcluster.Name]networkingv1.IngressStatus{}
	for k, v := range a.Annotations {
		status := networkingv1.IngressStatus{}
		if !strings.Contains(k, workload.InternalClusterStatusAnnotationPrefix) {
			continue
		}
		annotationParts := strings.Split(k, "/")
		if len(annotationParts) < 2 {
			return nil, fmt.Errorf("advanced scheduling annotation malformed %s value %s", workload.InternalClusterStatusAnnotationPrefix, a.Annotations[k])
		}
		clusterName := annotationParts[1]
		err := json.Unmarshal([]byte(v), &status)
		if err != nil {
			return statuses, err
		}
		statuses[logicalcluster.New(clusterName)] = status
	}

	cluster := logicalcluster.From(a)
	statuses[cluster] = a.Status
	return statuses, nil
}

func (a *Ingress) ProcessCustomHosts(ctx context.Context, dvs *v1.DomainVerificationList, _ CreateOrUpdateTraffic, _ DeleteTraffic, recordFunc getDNSrecordFunc) error {
	generatedHost, err := a.GetHCGhost(ctx, recordFunc)
	if err != nil || generatedHost == "" {
		if k8errors.IsNotFound(err) {
			return nil
		}
		return ErrGeneratedHostMissing
	}

	var unverifiedRules []networkingv1.IngressRule
	var verifiedRules []networkingv1.IngressRule

	//find any rules in the spec that are for unverifiedHosts that are not verified
	for _, rule := range a.Spec.Rules {
		//ignore any rules for generated unverifiedHosts (these are recalculated later)
		if rule.Host == generatedHost {
			continue
		}

		//check against domainverification status
		if IsDomainVerified(rule.Host, dvs.Items) || rule.Host == "" {
			verifiedRules = append(verifiedRules, rule)
		} else {
			//remove rule from accessor and mark it as awaiting verification
			unverifiedRules = append(unverifiedRules, rule)
		}

		//recalculate the generatedhost rule in the spec
		generatedHostRule := *rule.DeepCopy()
		generatedHostRule.Host = generatedHost
		verifiedRules = append(verifiedRules, generatedHostRule)
	}
	a.Spec.Rules = verifiedRules

	pending := &Pending{}
	var preservedPendingRules []networkingv1.IngressRule

	//test all the rules in the pending rules annotation to see if they are verified now
	pendingRulesRaw, exists := a.GetAnnotations()[ANNOTATION_PENDING_CUSTOM_HOSTS]
	if exists {
		if pendingRulesRaw != "" {
			err := json.Unmarshal([]byte(pendingRulesRaw), pending)
			if err != nil {
				return err
			}
		}
		for _, pendingRule := range pending.Rules {
			//recalculate the generatedhost rule in the spec
			generatedHostRule := *pendingRule.DeepCopy()
			generatedHostRule.Host = generatedHost
			a.Spec.Rules = append(a.Spec.Rules, generatedHostRule)

			//check against domainverification status
			if IsDomainVerified(pendingRule.Host, dvs.Items) || pendingRule.Host == "" {
				//add the rule to the spec
				a.Spec.Rules = append(a.Spec.Rules, pendingRule)
			} else {
				preservedPendingRules = append(preservedPendingRules, pendingRule)
			}
		}
	}
	//put the new unverified rules in the list of pending rules and update the annotation
	pending.Rules = append(preservedPendingRules, unverifiedRules...)
	if len(pending.Rules) > 0 {
		metadata.AddLabel(a, LABEL_HAS_PENDING_HOSTS, "true")
		newAnnotation, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		metadata.AddAnnotation(a, ANNOTATION_PENDING_CUSTOM_HOSTS, string(newAnnotation))
		return nil
	}
	metadata.RemoveLabel(a, LABEL_HAS_PENDING_HOSTS)
	metadata.RemoveAnnotation(a, ANNOTATION_PENDING_CUSTOM_HOSTS)
	return nil
}

func (a *Ingress) GetLogicalCluster() logicalcluster.Name {
	return logicalcluster.From(a)
}

func (a *Ingress) GetNamespaceName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: a.Namespace,
		Name:      a.Name,
	}
}

func (a *Ingress) String() string {
	return fmt.Sprintf("logical cluster: %v, kind: %v, namespace/name: %v", a.GetLogicalCluster(), a.GetKind(), a.GetNamespaceName())
}
