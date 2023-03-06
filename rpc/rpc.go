package rpc

import (
	"context"
	"github.com/mangohow/cloud-ide-k8s-operator/pb"
	"github.com/mangohow/cloud-ide-k8s-operator/rpc/middleware"
	"github.com/mangohow/cloud-ide-k8s-operator/service"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GrpcServer struct {
	client client.Client
	addr   string
}

func New(client client.Client, addr string) *GrpcServer {
	return &GrpcServer{client: client}
}

func (r *GrpcServer) Start(ctx context.Context) error {
	if r.addr == "" {
		r.addr = ":6387"
	}

	listener, err := net.Listen("tcp", r.addr)
	if err != nil {
		klog.Errorf("create grpc service: %v", err)
		return err
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(
		middleware.RecoveryInterceptorMiddleware(),
		middleware.LogInterceptorMiddleware(),
	))
	pb.RegisterCloudIdeServiceServer(server, service.NewWorkSpaceService(r.client))

	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()

	klog.Info("grpc server listening ...")
	if err := server.Serve(listener); err != nil {
		klog.Errorf("start grpc server: %v", err)
		return err
	}

	klog.Info("grpc server stopped")

	return nil
}
