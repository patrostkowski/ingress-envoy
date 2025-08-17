package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	netv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
)

func CreateKubeConfig() *kubernetes.Clientset {
	config, err := rest.InClusterConfig()
	if err == nil {
		if clientset, cerr := kubernetes.NewForConfig(config); cerr == nil {
			return clientset
		}
		klog.Error("failed to create client from in-cluster config", "error", err)
	} else {
		klog.Info("in-cluster config not available, falling back to kubeconfig")
	}

	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil
	}
	return clientset
}

func getSelfServiceInfo() (string, error) {
	podName := os.Getenv("POD_NAME")
	ns := os.Getenv("POD_NAMESPACE")
	if podName == "" || ns == "" {
		return "", fmt.Errorf("POD_NAME or POD_NAMESPACE not set")
	}
	svcAddr := fmt.Sprintf("%s.%s.svc.cluster.local", podSuffix(podName), ns)
	return svcAddr, nil
}

func (c *IngressController) listObservedIngresses(ctx context.Context) ([]netv1.Ingress, error) {
	list, err := c.client.NetworkingV1().Ingresses(v1.NamespaceAll).List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make([]netv1.Ingress, 0, len(list.Items))
	for i := range list.Items {
		ing := &list.Items[i]
		if c.matchesIngressClass(ing) {
			out = append(out, *ing)
		}
	}
	return out, nil
}

func (c *IngressController) matchesIngressClass(ing *netv1.Ingress) bool {
	if ing.Spec.IngressClassName != nil {
		return *ing.Spec.IngressClassName == c.ingressClass
	}
	if cls, ok := ing.Annotations[legacyIngressClassAnnotation]; ok {
		return cls == c.ingressClass
	}
	return false
}
