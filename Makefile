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
