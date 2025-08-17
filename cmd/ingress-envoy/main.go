package main

import (
	"github.com/patrostkowski/ingress-envoy/internal/controller"
)

func main() {
	client := controller.CreateKubeConfig()
	ctrl := controller.NewIngressController(client)
	if err := ctrl.Run(); err != nil {
		panic(err)
	}
}
