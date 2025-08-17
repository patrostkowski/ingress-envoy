package controller

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	xdscache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	netlisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type IngressController struct {
	client       kubernetes.Interface
	factory      informers.SharedInformerFactory
	informer     cache.SharedIndexInformer
	queue        workqueue.TypedRateLimitingInterface[Request]
	lister       netlisters.IngressLister
	mu           *sync.RWMutex
	xdscache     xdscache.SnapshotCache
	ingressClass string
	xdsCache     map[Request]*XdsBundle
}

func NewIngressController(client kubernetes.Interface) *IngressController {
	if client == nil {
		panic("client must not be nil")
	}

	factory := informers.NewSharedInformerFactory(client, 0)
	ingressInformer := factory.Networking().V1().Ingresses()

	queue := workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[Request](),
	)

	ctrl := &IngressController{
		client:       client,
		factory:      factory,
		informer:     ingressInformer.Informer(),
		lister:       ingressInformer.Lister(),
		queue:        queue,
		mu:           &sync.RWMutex{},
		xdscache:     xdscache.NewSnapshotCache(false, xdscache.IDHash{}, nil),
		ingressClass: DefaultName,
		xdsCache:     make(map[Request]*XdsBundle),
	}

	ctrl.informer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: ctrl.isObservedIngressClass,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    ctrl.enqueue,
			UpdateFunc: func(_, newObj interface{}) { ctrl.enqueue(newObj) },
			DeleteFunc: ctrl.enqueue,
		},
	})

	return ctrl
}

func (c *IngressController) enqueue(obj interface{}) {
	if req, ok := toRequest(obj); ok {
		klog.Infof("enqueue %s/%s", req.Namespace, req.Name)
		c.queue.Add(req)
	}
}

func toRequest(obj interface{}) (Request, bool) {
	if tomb, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tomb.Obj
	}
	accessor, err := meta.Accessor(obj)
	if err != nil {
		klog.Errorf("enqueue: cannot get meta: %v", err)
		return Request{}, false
	}
	return Request{Namespace: accessor.GetNamespace(), Name: accessor.GetName()}, true
}

func (c *IngressController) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer runtime.HandleCrashWithContext(ctx)
	defer c.queue.ShutDown()

	klog.Info("starting ingress controller")

	go c.informer.Run(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced) {
		return fmt.Errorf("timed out waiting for caches to sync")
	}

	if err := c.buildIngressEnvoyCacheMapping(ctx); err != nil {
		return err
	}

	go wait.UntilWithContext(ctx, c.runWorker, time.Second)

	go c.runEnvoyDataPlane(ctx)

	go func() {
		if err := c.startHTTPServer(ctx); err != nil {
			stop()
		}
	}()

	<-ctx.Done()

	klog.Info("shutting down ingress controller")

	return nil
}

func (c *IngressController) runWorker(ctx context.Context) {
	for {
		req, shutdown := c.queue.Get()
		if shutdown {
			return
		}
		func() {
			defer c.queue.Done(req)
			res, err := c.Reconcile(ctx, req)
			if err != nil {
				runtime.HandleError(fmt.Errorf("reconcile %s/%s: %v", req.Namespace, req.Name, err))
				c.queue.AddRateLimited(req)
				return
			}
			if res.RequeueAfter > 0 {
				c.queue.Forget(req)
				c.queue.AddAfter(req, res.RequeueAfter)
				return
			}
			if res.Requeue {
				c.queue.AddRateLimited(req)
				return
			}
			c.queue.Forget(req)
		}()
	}
}

func (c *IngressController) startHTTPServer(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: ":8080", Handler: mux}
	klog.Info("starting HTTP server on :8080")

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		klog.Info("shutting down HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http server shutdown failed: %w", err)
		}
		klog.Info("HTTP server shut down cleanly")
		return nil
	case err := <-errCh:
		return err
	}
}
