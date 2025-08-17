package controller

import (
	"context"
	"fmt"
	"time"

	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
)

func (c *IngressController) Reconcile(ctx context.Context, req Request) (Result, error) {
	ing, err := c.lister.Ingresses(req.Namespace).Get(req.Name)
	if apierrors.IsNotFound(err) {
		klog.Infof("Ingress %s/%s deleted", req.Namespace, req.Name)
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}

	if c.getIngressClass(ing) != IngressClass {
		return Result{}, nil
	}

	klog.Infof("Generated Envoy config for %s/%s:\n%s", req.Namespace, req.Name, &ing.Spec)

	if err := c.pushSnapshot(); err != nil {
		return Result{}, fmt.Errorf("push snapshot: %w", err)
	}

	return Result{}, nil
}

func (c *IngressController) buildIngressEnvoyCacheMapping(ctx context.Context) error {
	klog.Info("building ingress to xDS mapping")

	ingresses, err := c.listObservedIngresses(ctx)
	if err != nil {
		return fmt.Errorf("listObservedIngresses failed: %w", err)
	}

	c.xdsCache = make(map[Request]*XdsBundle)

	var clusters []types.Resource
	var vhosts []*routev3.VirtualHost

	for _, ingress := range ingresses {
		klog.Infof("building xds config for %v/%v", &ingress.ObjectMeta.Namespace, &ingress.ObjectMeta.Name)
		for _, rule := range ingress.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}

			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service == nil {
					continue
				}
				cluster := buildEnvoyClusterBasedOnIngressServiceBackend(ingress.Namespace, path.Backend.Service)
				clusters = append(clusters, cluster)
				klog.Infof("built cluster for %v/%v: %v", &ingress.ObjectMeta.Namespace, &ingress.ObjectMeta.Name, cluster)
			}

			vhost := buildEnvoyVirtualHostFromIngressRule(ingress.Namespace, rule)
			vhosts = append(vhosts, vhost)
			klog.Infof("built vhost for %v/%v: %v", &ingress.ObjectMeta.Namespace, &ingress.ObjectMeta.Name, vhost)
		}
	}

	listener, err := buildHTTPListener(vhosts)
	if err != nil {
		return err
	}
	klog.Infof("built listener: %v", listener)

	bundle := &XdsBundle{
		Clusters:  clusters,
		Listeners: []types.Resource{listener},
		Endpoints: []types.Resource{},
		Routes:    []types.Resource{},
		Version:   fmt.Sprintf("v%d", time.Now().UnixNano()),
	}

	c.xdsCache[Request{Name: "global"}] = bundle

	return nil
}
