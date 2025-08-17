#!/bin/bash

kubectl apply -f example/deploy/raw

helm repo add metallb https://metallb.github.io/metallb
helm upgrade --install metallb metallb/metallb -n metallb-system --create-namespace --wait

cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: ingress
  namespace: metallb-system
spec:
  addresses:
  - 172.18.0.100-172.18.0.199
EOF

cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: ingress
  namespace: metallb-system
EOF

helm repo add matheusfm https://matheusfm.dev/charts
helm upgrade --install httpbin matheusfm/httpbin -n httpbin --create-namespace --wait
helm upgrade --install nginx oci://registry-1.docker.io/bitnamicharts/nginx -n nginx --create-namespace --wait

