package controller

import (
	"context"
	"testing"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	netv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

var TestIngresses = []runtime.Object{
	&netv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Name:      "match-v1",
			Namespace: "ns1",
		},
		Spec: netv1.IngressSpec{
			IngressClassName: ptr.To("my-class"),
			Rules: []netv1.IngressRule{
				{
					Host: "1.example.local",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: ptr.To(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "svc-v1",
											Port: netv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
	&netv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Name:      "match-legacy",
			Namespace: "ns2",
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "my-class",
			},
		},
		Spec: netv1.IngressSpec{
			Rules: []netv1.IngressRule{
				{
					Host: "2.example.local",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: ptr.To(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "svc-legacy",
											Port: netv1.ServiceBackendPort{Number: 8080},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
	&netv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Name:      "other-class",
			Namespace: "ns3",
		},
		Spec: netv1.IngressSpec{
			IngressClassName: ptr.To("not-my-class"),
			Rules: []netv1.IngressRule{
				{
					Host: "3.example.local",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: ptr.To(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "svc-other",
											Port: netv1.ServiceBackendPort{Number: 9090},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

var TestEnvoyConfigs = &XdsBundle{
	Clusters: []types.Resource{
		&clusterv3.Cluster{
			Name:                 "svc-v1_80",
			ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STRICT_DNS},
			ConnectTimeout:       durationpb.New(1 * time.Second),
			LoadAssignment: &endpointv3.ClusterLoadAssignment{
				ClusterName: "svc-v1_80",
				Endpoints: []*endpointv3.LocalityLbEndpoints{{
					LbEndpoints: []*endpointv3.LbEndpoint{{
						HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
							Endpoint: &endpointv3.Endpoint{
								Address: &corev3.Address{
									Address: &corev3.Address_SocketAddress{
										SocketAddress: &corev3.SocketAddress{
											Address: "svc-v1.ns1.svc.cluster.local",
											PortSpecifier: &corev3.SocketAddress_PortValue{
												PortValue: 80,
											},
										},
									},
								},
							},
						},
					}},
				}},
			},
		},
		&clusterv3.Cluster{
			Name:                 "svc-legacy_8080",
			ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STRICT_DNS},
			ConnectTimeout:       durationpb.New(1 * time.Second),
			LoadAssignment: &endpointv3.ClusterLoadAssignment{
				ClusterName: "svc-legacy_8080",
				Endpoints: []*endpointv3.LocalityLbEndpoints{{
					LbEndpoints: []*endpointv3.LbEndpoint{{
						HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
							Endpoint: &endpointv3.Endpoint{
								Address: &corev3.Address{
									Address: &corev3.Address_SocketAddress{
										SocketAddress: &corev3.SocketAddress{
											Address: "svc-legacy.ns2.svc.cluster.local",
											PortSpecifier: &corev3.SocketAddress_PortValue{
												PortValue: 8080,
											},
										},
									},
								},
							},
						},
					}},
				}},
			},
		},
	},
	Listeners: []types.Resource{
		func() *listenerv3.Listener {
			vhosts := []*routev3.VirtualHost{
				{
					Name:    "1.example.local",
					Domains: []string{"1.example.local"},
					Routes: []*routev3.Route{{
						Match: &routev3.RouteMatch{
							PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &routev3.Route_Route{
							Route: &routev3.RouteAction{
								ClusterSpecifier: &routev3.RouteAction_Cluster{
									Cluster: "svc-v1_80",
								},
							},
						},
					}},
				},
				{
					Name:    "2.example.local",
					Domains: []string{"2.example.local"},
					Routes: []*routev3.Route{{
						Match: &routev3.RouteMatch{
							PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &routev3.Route_Route{
							Route: &routev3.RouteAction{
								ClusterSpecifier: &routev3.RouteAction_Cluster{
									Cluster: "svc-legacy_8080",
								},
							},
						},
					}},
				},
			}

			routeCfg := &routev3.RouteConfiguration{
				Name:         "local_route",
				VirtualHosts: vhosts,
			}

			hcm := &hcmv3.HttpConnectionManager{
				StatPrefix: "ingress_http",
				RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{
					RouteConfig: routeCfg,
				},
				HttpFilters: []*hcmv3.HttpFilter{{
					Name: "envoy.filters.http.router",
				}},
			}

			hcmAny, _ := anypb.New(hcm)

			return &listenerv3.Listener{
				Name: "listener_0",
				Address: &corev3.Address{
					Address: &corev3.Address_SocketAddress{
						SocketAddress: &corev3.SocketAddress{
							Address: "0.0.0.0",
							PortSpecifier: &corev3.SocketAddress_PortValue{
								PortValue: 80,
							},
						},
					},
				},
				FilterChains: []*listenerv3.FilterChain{{
					Filters: []*listenerv3.Filter{{
						Name:       "envoy.filters.network.http_connection_manager",
						ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcmAny},
					}},
				}},
			}
		}(),
	},
	Version: "test-version",
}

func TestBuildIngressEnvoyCacheMapping(t *testing.T) {
	client := k8sfake.NewSimpleClientset(TestIngresses...)
	ctrl := NewIngressController(client)
	ctrl.ingressClass = "my-class"

	if err := ctrl.buildIngressEnvoyCacheMapping(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildIngressEnvoyCacheMapping_MatchesExpected(t *testing.T) {
	t.Helper()

	client := k8sfake.NewSimpleClientset(TestIngresses...)
	ctrl := NewIngressController(client)
	ctrl.ingressClass = "my-class"

	if err := ctrl.buildIngressEnvoyCacheMapping(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := ctrl.xdsCache[Request{Name: "global"}]
	if !ok {
		t.Fatalf("expected xdsCache to have key {Name: \"global\"}")
	}

	wantClusters := map[string]*clusterv3.Cluster{}
	for _, r := range TestEnvoyConfigs.Clusters {
		c, ok := r.(*clusterv3.Cluster)
		if !ok {
			t.Fatalf("expected *clusterv3.Cluster in TestEnvoyConfigs.Clusters, got %T", r)
		}
		wantClusters[c.GetName()] = c
	}

	if len(got.Clusters) != len(wantClusters) {
		t.Fatalf("cluster count mismatch: got=%d want=%d", len(got.Clusters), len(wantClusters))
	}

	for _, r := range got.Clusters {
		ac, ok := r.(*clusterv3.Cluster)
		if !ok {
			t.Fatalf("expected *clusterv3.Cluster in got.Clusters, got %T", r)
		}
		wc, ok := wantClusters[ac.GetName()]
		if !ok {
			t.Fatalf("unexpected cluster built: %q", ac.GetName())
		}

		if ac.GetName() != wc.GetName() {
			t.Errorf("cluster name: got=%q want=%q", ac.GetName(), wc.GetName())
		}
		if ac.GetType() != wc.GetType() {
			t.Errorf("cluster %q type: got=%v want=%v", ac.GetName(), ac.GetType(), wc.GetType())
		}
		if ac.GetConnectTimeout().AsDuration() != wc.GetConnectTimeout().AsDuration() {
			t.Errorf("cluster %q connect_timeout: got=%v want=%v", ac.GetName(), ac.GetConnectTimeout(), wc.GetConnectTimeout())
		}

		ga := firstSocketAddressFromCluster(ac)
		wa := firstSocketAddressFromCluster(wc)
		if ga == nil || wa == nil {
			t.Fatalf("nil socket address: got=%v want=%v", ga, wa)
		}
		if ga.GetAddress() != wa.GetAddress() || ga.GetPortValue() != wa.GetPortValue() {
			t.Errorf("cluster %q socket: got=%s:%d want=%s:%d",
				ac.GetName(), ga.GetAddress(), ga.GetPortValue(), wa.GetAddress(), wa.GetPortValue())
		}
	}

	if len(got.Listeners) != 1 {
		t.Fatalf("listener count mismatch: got=%d want=1", len(got.Listeners))
	}
	gl, ok := got.Listeners[0].(*listenerv3.Listener)
	if !ok {
		t.Fatalf("expected *listenerv3.Listener, got %T", got.Listeners[0])
	}
	sa := gl.GetAddress().GetSocketAddress()
	if sa == nil || sa.GetAddress() != "0.0.0.0" || sa.GetPortValue() != 80 {
		t.Errorf("listener address: got=%v want=0.0.0.0:80", sa)
	}

	hcm := mustGetHCMFromListener(t, gl)
	if hcm.GetStatPrefix() != "ingress_http" {
		t.Errorf("HCM stat_prefix: got=%q want=%q", hcm.GetStatPrefix(), "ingress_http")
	}

	if !hasHTTPFilter(hcm, "envoy.filters.http.router") {
		t.Errorf("HCM is missing router filter")
	}

	rc := hcm.GetRouteConfig()
	if rc == nil {
		t.Fatalf("expected inline route_config, got nil (SRDS/RDS not used)")
	}
	if rc.GetName() != "local_route" {
		t.Errorf("route_config name: got=%q want=%q", rc.GetName(), "local_route")
	}

	wantHostCluster := map[string]string{}
	for _, r := range TestEnvoyConfigs.Listeners {
		l, _ := r.(*listenerv3.Listener)
		ehcm := mustGetHCMFromListener(t, l)
		erc := ehcm.GetRouteConfig()
		for _, vh := range erc.GetVirtualHosts() {
			for _, route := range vh.GetRoutes() {
				if cl := route.GetRoute().GetCluster(); cl != "" {
					if len(vh.GetDomains()) > 0 {
						wantHostCluster[vh.GetDomains()[0]] = cl
					}
				}
			}
		}
	}

	gotHostCluster := map[string]string{}
	for _, vh := range rc.GetVirtualHosts() {
		for _, route := range vh.GetRoutes() {
			if cl := route.GetRoute().GetCluster(); cl != "" && len(vh.GetDomains()) > 0 {
				gotHostCluster[vh.GetDomains()[0]] = cl
			}
		}
	}

	if len(gotHostCluster) != len(wantHostCluster) {
		t.Fatalf("vhost count mismatch: got=%d want=%d (got=%v want=%v)", len(gotHostCluster), len(wantHostCluster), gotHostCluster, wantHostCluster)
	}
	for host, wantCluster := range wantHostCluster {
		gotCluster, ok := gotHostCluster[host]
		if !ok {
			t.Errorf("missing vhost for host %q", host)
			continue
		}
		if gotCluster != wantCluster {
			t.Errorf("host %q cluster: got=%q want=%q", host, gotCluster, wantCluster)
		}
	}

	if len(got.Routes) != 0 {
		t.Errorf("expected no standalone Route resources, got=%d", len(got.Routes))
	}
}

func firstSocketAddressFromCluster(c *clusterv3.Cluster) *corev3.SocketAddress {
	if c.GetLoadAssignment() == nil || len(c.GetLoadAssignment().GetEndpoints()) == 0 {
		return nil
	}
	les := c.GetLoadAssignment().GetEndpoints()[0]
	if len(les.GetLbEndpoints()) == 0 {
		return nil
	}
	ep := les.GetLbEndpoints()[0].GetEndpoint()
	if ep == nil || ep.GetAddress() == nil {
		return nil
	}
	return ep.GetAddress().GetSocketAddress()
}

func mustGetHCMFromListener(t *testing.T, l *listenerv3.Listener) *hcmv3.HttpConnectionManager {
	t.Helper()
	if len(l.GetFilterChains()) == 0 || len(l.GetFilterChains()[0].GetFilters()) == 0 {
		t.Fatalf("listener has no filters")
	}
	f := l.GetFilterChains()[0].GetFilters()[0]
	if f.GetName() != "envoy.filters.network.http_connection_manager" {
		t.Fatalf("expected HCM filter, got %q", f.GetName())
	}
	var hcm hcmv3.HttpConnectionManager
	switch cfg := f.GetConfigType().(type) {
	case *listenerv3.Filter_TypedConfig:
		if cfg.TypedConfig == nil {
			t.Fatalf("HCM has nil typed config")
		}
		if err := anypb.UnmarshalTo(cfg.TypedConfig, &hcm, proto.UnmarshalOptions{}); err != nil {
			t.Fatalf("failed to unmarshal HCM: %v", err)
		}
	default:
		t.Fatalf("unexpected HCM config type: %T", cfg)
	}
	return &hcm
}

func hasHTTPFilter(hcm *hcmv3.HttpConnectionManager, name string) bool {
	for _, hf := range hcm.GetHttpFilters() {
		if hf.GetName() == name {
			return true
		}
	}
	return false
}
