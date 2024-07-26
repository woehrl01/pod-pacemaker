# Variables that are used in `defined<>endef` blocks need to be exported
export VERSION ?= v0.0.1
export REGISTRY ?= ghcr.io
IMAGE_BUILDER ?= docker
IMAGE_BUILD_CMD ?= buildx build
export IMAGE_NAME ?= woehrl01/pod-pacemaker

export IMG = $(REGISTRY)/$(IMAGE_NAME):$(VERSION)
export CGO_ENABLED=0
export GOOS=linux

.PHONY: proto
.ONESHELL:
SHELL := /bin/bash

define kind_load =
	# The architecture to load depends on the current environment (gitlab runner/workstation)
	arch=$(uname -m)
	case ${arch} in
		x86_64)
			kind load docker-image ${IMG}-amd64
		;;
		aarch64)
			kind load docker-image ${IMG}-arm64
		;;
		*)
			echo "Architecture ${arch} not supported"
			exit 1
		;;
	esac
endef

define kind_deploy =
	# The architecture to load depends on the current environment (gitlab runner/workstation)
	arch=$(uname -m)
	case ${arch} in
		x86_64)
			tag=${VERSION}-amd64
		;;
		aarch64)
			tag=${VERSION}-arm64
		;;
		*)
			echo "Architecture ${arch} not supported"
			exit 1
		;;
	esac
	helm upgrade --install pod-pacemaker charts/pod-pacemaker \
		--set image.repository=${REGISTRY}/${IMAGE_NAME} \
		--set image.tag=${tag} \
		--set debugLogging=true \
		--set defaultThrottleConfig.config.maxConcurrent.value=1
	env > makefilenv
endef

# .SILENT: kind-deploy kind-load

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
	# Build each supported architecture and load the resulting image into docker for use with kind
	docker buildx create --use
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) --load --platform linux/amd64 --tag $(IMG)-amd64 . 
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) --load --platform linux/arm64/v8 --tag $(IMG)-arm64 .

docker-push:
	# Build (again) for all supported architectures and pushlish the result
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) --push --platform linux/arm64/v8,linux/amd64 --tag $(IMG) .

helm-render:
	helm template charts/pod-pacemaker --set image.repository=$(REGISTRY)/$(IMAGE_NAME) --set image.tag=$(VERSION)

manifests:
	controller-gen crd paths="./..." output:crd:artifacts:config=charts/pod-pacemaker/crds

helm-push:
	helm package charts/pod-pacemaker --app-version $(VERSION) --version $(VERSION)
	helm push pod-pacemaker-*.tgz oci://ghcr.io/woehrl01/pod-pacemaker

kind-load: ; $(value kind_load)

kind-deploy: ; $(value kind_deploy)

