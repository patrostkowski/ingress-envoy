package controller

import (
	"fmt"
	"time"

	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	alsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/stream/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	netv1 "k8s.io/api/networking/v1"
)

const (
	EnvoyDefaultAdminListenAddr = "0.0.0.0"
	EnvoyDefaultAdminListenPort = 15000
	EnvoyDefaultXDSClusterName  = "xds_controller"

	EnvoyDataPlaneDefaultListerPort = 18000
)

func buildEnvoyClusterBasedOnIngressServiceBackend(ns string, ing *netv1.IngressServiceBackend) *clusterv3.Cluster {
	name := ing.Name
	port := ing.Port.Number
	clusterName := fmt.Sprintf("%s_%d", name, port)

	return &clusterv3.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STRICT_DNS},
		ConnectTimeout:       durationpb.New(1 * time.Second),
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: &corev3.Address{
										Address: &corev3.Address_SocketAddress{
											SocketAddress: &corev3.SocketAddress{
												Address: fmt.Sprintf("%s.%s.svc.cluster.local", name, ns),
												PortSpecifier: &corev3.SocketAddress_PortValue{
													PortValue: uint32(port),
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
		},
	}
}

func buildEnvoyVirtualHostFromIngressRule(ns string, rule netv1.IngressRule) *routev3.VirtualHost {
	vh := &routev3.VirtualHost{
		Name:    rule.Host,
		Domains: []string{rule.Host},
		Routes:  []*routev3.Route{},
	}

	if rule.HTTP != nil {
		for _, p := range rule.HTTP.Paths {
			clusterName := fmt.Sprintf("%s_%d", p.Backend.Service.Name, p.Backend.Service.Port.Number)

			route := &routev3.Route{
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_Prefix{
						Prefix: p.Path,
					},
				},
				Action: &routev3.Route_Route{
					Route: &routev3.RouteAction{
						ClusterSpecifier: &routev3.RouteAction_Cluster{
							Cluster: clusterName,
						},
					},
				},
			}

			vh.Routes = append(vh.Routes, route)
		}
	}

	return vh
}

func buildHTTPListener(vhosts []*routev3.VirtualHost) (*listenerv3.Listener, error) {
	routeCfg := &routev3.RouteConfiguration{
		Name:         "local_route",
		VirtualHosts: vhosts,
	}

	tc, _ := anypb.New(&routerv3.Router{})
	als, _ := anypb.New(&alsv3.StdoutAccessLog{
		AccessLogFormat: &alsv3.StdoutAccessLog_LogFormat{
			LogFormat: &corev3.SubstitutionFormatString{
				Format: &corev3.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &corev3.DataSource{
						Specifier: &corev3.DataSource_InlineString{
							InlineString: "[access] [%START_TIME%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% %PROTOCOL%\" " +
								"%RESPONSE_CODE% %RESPONSE_FLAGS% " +
								"hostname=%REQ(:AUTHORITY)% " +
								"upstream=%UPSTREAM_HOST% " +
								"request_id=%REQ(X-REQUEST-ID)% " +
								"bytes_sent=%BYTES_SENT% bytes_received=%BYTES_RECEIVED% " +
								"duration_ms=%DURATION% " +
								"user_agent=\"%REQ(USER-AGENT)%\" " +
								"x_forwarded_for=\"%REQ(X-FORWARDED-FOR)%\"\n",
						},
					},
				},
			},
		},
	})

	hcm := &hcmv3.HttpConnectionManager{
		StatPrefix: "ingress_http",
		RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{
			RouteConfig: routeCfg,
		},
		HttpFilters: []*hcmv3.HttpFilter{
			{
				Name: "envoy.filters.http.router",
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: tc,
				},
			},
		},
		AccessLog: []*accesslogv3.AccessLog{
			{
				Name: "envoy.access_loggers.stdout",
				ConfigType: &accesslogv3.AccessLog_TypedConfig{
					TypedConfig: als,
				},
			},
		},
	}

	hcmAny, err := anypb.New(hcm)
	if err != nil {
		return nil, err
	}

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
		FilterChains: []*listenerv3.FilterChain{
			{
				Filters: []*listenerv3.Filter{
					{
						Name:       "envoy.filters.network.http_connection_manager",
						ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcmAny},
					},
				},
			},
		},
	}, nil
}
