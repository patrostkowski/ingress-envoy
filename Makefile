BUILD_BIN_DIR=./bin
PWD := $(shell pwd)
DOCKER_IMAGE_NAME=patrostkowski/ingress-envoy

.PHONY: all generate run build-all build build-cli docker-build docker-run test test-e2e bench create-cluster clean

all: build-all

run:
	$(MAKE) build
	exec ${BUILD_BIN_DIR}/ingress-envoy

build:
	go build -o ${BUILD_BIN_DIR}/ingress-envoy ./cmd/ingress-envoy

docker-build:
	docker build -t ${DOCKER_IMAGE_NAME} .

lint-verify:
	golangci-lint config verify

lint:
	golangci-lint run

check: lint-verify lint test

license:
	addlicense -c "Patryk Rostkowski" -l apache -y 2025 .

test:
	go test ./...

test-e2e:
	go test ./tests/e2e

create-cluster:
	kind create cluster

clean:
	rm -rf ${BUILD_BIN_DIR}
	git clean -xdff