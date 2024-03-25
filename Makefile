VERSION ?= v0.0.1
REGISTRY ?= ghcr.io
IMAGE_BUILDER ?= docker
IMAGE_BUILD_CMD ?= build
IMAGE_NAME ?= woehrl01/pod-pacemaker

export IMG = $(REGISTRY)/$(IMAGE_NAME):$(VERSION)
export CGO_ENABLED=0
export GOOS=linux

.PHONY: proto

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/pod_limiter.proto

cni:
	cd cmd/cni-plugin && go build -o ../../bin/cni-plugin

make-init:
	cd cmd/init && go build -o ../../bin/cni-init

daemonset:
	cd cmd/node-daemon && go build -o ../../bin/node-daemon

build: cni make-init daemonset

clean:
	rm -rf bin/*

docker-build:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) -t $(IMG) .

docker-push:
	$(IMAGE_BUILDER) push $(IMG)

helm-render:
	helm template charts/pod-pacemaker --set image.repository=$(REGISTRY)/$(IMAGE_NAME) --set image.tag=$(VERSION)

manifests:
	controller-gen crd paths="./..." output:crd:artifacts:config=charts/pod-pacemaker/crds

helm-push:
	helm package charts/pod-pacemaker --app-version $(VERSION) --version $(VERSION)
	helm push pod-pacemaker-*.tgz oci://ghcr.io/woehrl01/pod-pacemaker
