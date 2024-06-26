// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v4.25.3
// source: proto/pod_limiter.proto

package podlimiter_proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	PodLimiter_Wait_FullMethodName = "/podlimiter.PodLimiter/Wait"
)

// PodLimiterClient is the client API for PodLimiter service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type PodLimiterClient interface {
	Wait(ctx context.Context, in *WaitRequest, opts ...grpc.CallOption) (*WaitResponse, error)
}

type podLimiterClient struct {
	cc grpc.ClientConnInterface
}

func NewPodLimiterClient(cc grpc.ClientConnInterface) PodLimiterClient {
	return &podLimiterClient{cc}
}

func (c *podLimiterClient) Wait(ctx context.Context, in *WaitRequest, opts ...grpc.CallOption) (*WaitResponse, error) {
	out := new(WaitResponse)
	err := c.cc.Invoke(ctx, PodLimiter_Wait_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PodLimiterServer is the server API for PodLimiter service.
// All implementations must embed UnimplementedPodLimiterServer
// for forward compatibility
type PodLimiterServer interface {
	Wait(context.Context, *WaitRequest) (*WaitResponse, error)
	mustEmbedUnimplementedPodLimiterServer()
}

// UnimplementedPodLimiterServer must be embedded to have forward compatible implementations.
type UnimplementedPodLimiterServer struct {
}

func (UnimplementedPodLimiterServer) Wait(context.Context, *WaitRequest) (*WaitResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Wait not implemented")
}
func (UnimplementedPodLimiterServer) mustEmbedUnimplementedPodLimiterServer() {}

// UnsafePodLimiterServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to PodLimiterServer will
// result in compilation errors.
type UnsafePodLimiterServer interface {
	mustEmbedUnimplementedPodLimiterServer()
}

func RegisterPodLimiterServer(s grpc.ServiceRegistrar, srv PodLimiterServer) {
	s.RegisterService(&PodLimiter_ServiceDesc, srv)
}

func _PodLimiter_Wait_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WaitRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PodLimiterServer).Wait(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: PodLimiter_Wait_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PodLimiterServer).Wait(ctx, req.(*WaitRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// PodLimiter_ServiceDesc is the grpc.ServiceDesc for PodLimiter service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var PodLimiter_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "podlimiter.PodLimiter",
	HandlerType: (*PodLimiterServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Wait",
			Handler:    _PodLimiter_Wait_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/pod_limiter.proto",
}
