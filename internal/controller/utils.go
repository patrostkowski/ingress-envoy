package controller

import (
	"strings"

	netv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	legacyIngressClassAnnotation = "kubernetes.io/ingress.class"
)

func podSuffix(name string) string {
	parts := strings.Split(name, "-")
	suffix := strings.Join(parts[len(parts)-2:], "-")
	return suffix
}

func (c *IngressController) isObservedIngressClass(obj interface{}) bool {
	ing, ok := obj.(*netv1.Ingress)
	if !ok {
		tomb, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return false
		}
		ing, ok = tomb.Obj.(*netv1.Ingress)
		if !ok {
			return false
		}
	}
	return c.matchesClass(ing)
}

func (c *IngressController) matchesClass(ing *netv1.Ingress) bool {
	if c.ingressClass == "" {
		return true
	}
	if ing.Spec.IngressClassName != nil {
		return *ing.Spec.IngressClassName == c.ingressClass
	}
	if cls, ok := ing.Annotations["kubernetes.io/ingress.class"]; ok {
		return cls == c.ingressClass
	}
	return false
}

func (c *IngressController) getIngressClass(ing *netv1.Ingress) string {
	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != "" {
		return *ing.Spec.IngressClassName
	}
	if v, ok := ing.Annotations[legacyIngressClassAnnotation]; ok {
		return v
	}
	return ""
}
