VERSION ?= v0.0.1
REGISTRY ?= ghcr.io
IMAGE_BUILDER ?= docker
IMAGE_BUILD_CMD ?= build
IMAGE_NAME ?= woehrl01/kubelet-throttler

export IMG = $(REGISTRY)/$(IMAGE_NAME):$(VERSION)

.PHONY: proto

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/pod_limiter.proto

cni:
	cd cmd/cni-plugin && CGO_ENABLED=0 GOOS=linux go build -o ../../bin/cni-plugin

make-init:
	cd cmd/init && CGO_ENABLED=0 GOOS=linux go build -o ../../bin/cni-init

daemonset:
	cd cmd/node-daemon && CGO_ENABLED=0 GOOS=linux go build -o ../../bin/node-daemon

build: cni make-init daemonset

clean:
	rm -rf bin/*

docker-build:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) -t $(IMG) .
