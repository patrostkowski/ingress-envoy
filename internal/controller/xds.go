package controller

import (
	"context"
	"fmt"
	"net"
	"time"

	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	runtimeservice "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	secretservice "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	xdscache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	xdsserver "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	xdstest "github.com/envoyproxy/go-control-plane/pkg/test/v3"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

func (c *IngressController) runEnvoyDataPlane(ctx context.Context) {
	cb := &xdstest.Callbacks{}
	srv := xdsserver.NewServer(ctx, c.xdscache, cb)

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams))
	grpcServer := grpc.NewServer(grpcOptions...)

	lis, err := net.Listen("tcp", ":18000")
	if err != nil {
		panic(err)
	}

	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, srv)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, srv)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, srv)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, srv)
	secretservice.RegisterSecretDiscoveryServiceServer(grpcServer, srv)
	runtimeservice.RegisterRuntimeDiscoveryServiceServer(grpcServer, srv)

	go func() {
		<-ctx.Done()
		klog.Info("shutting down gRPC xDS server...")
		grpcServer.GracefulStop()
		klog.Info("gRPC xDS server shut down cleanly")
	}()

	klog.Info("xds server listening on :18000")
	if err = grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		klog.Errorf("gRPC server exited with error: %v", err)
	}
}

func (c *IngressController) pushSnapshot() error {
	version := fmt.Sprintf("%d", time.Now().UnixNano())

	var endpoints []types.Resource
	var clusters []types.Resource
	var listeners []types.Resource
	var routes []types.Resource

	for _, bundle := range c.xdsCache {
		for _, e := range bundle.Endpoints {
			endpoints = append(endpoints, e)
		}
		for _, cl := range bundle.Clusters {
			clusters = append(clusters, cl)
		}
		for _, l := range bundle.Listeners {
			listeners = append(listeners, l)
		}
		for _, r := range bundle.Routes {
			routes = append(routes, r)
		}
	}

	snapshot, err := xdscache.NewSnapshot(version, map[string][]types.Resource{
		resource.EndpointType: endpoints,
		resource.ClusterType:  clusters,
		resource.ListenerType: listeners,
		resource.RouteType:    routes,
	})
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	if err := c.xdscache.SetSnapshot(context.Background(), DefaultName, snapshot); err != nil {
		return fmt.Errorf("failed to set snapshot: %w", err)
	}

	klog.Infof(
		"pushed snapshot v%s (clusters=%d, listeners=%d, routes=%d, endpoints=%d)",
		version, len(clusters), len(listeners), len(routes), len(endpoints),
	)
	return nil
}
