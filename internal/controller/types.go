package controller

import (
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
)

const (
	grpcMaxConcurrentStreams = 1000000
	IngressClass             = "ingress-envoy"
	DefaultName              = "ingress-envoy"
)

type Request struct{ Namespace, Name string }

type Result struct {
	Requeue      bool
	RequeueAfter time.Duration
}

type XdsBundle struct {
	Endpoints []types.Resource
	Clusters  []types.Resource
	Routes    []types.Resource
	Listeners []types.Resource
	Version   string
}
