package app

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	grpcapi "plasmod/src/internal/api/grpc"
	plasmodv1 "plasmod/src/internal/api/grpc/pb/plasmod/v1"
	"plasmod/src/internal/access"
)

// GRPCConfig holds optional gRPC listener settings.
type GRPCConfig struct {
	Enabled bool
	Addr    string
}

// ResolveGRPCConfig reads PLASMOD_GRPC_* env vars.
// gRPC is enabled by default on 0.0.0.0:19531 (distinct from HTTP API :19530).
// Set PLASMOD_GRPC_ENABLED=0 to disable.
func ResolveGRPCConfig() GRPCConfig {
	raw := strings.TrimSpace(os.Getenv("PLASMOD_GRPC_ENABLED"))
	if raw != "" && (raw == "0" || strings.EqualFold(raw, "false") || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "no")) {
		return GRPCConfig{Enabled: false}
	}
	addr := strings.TrimSpace(os.Getenv("PLASMOD_GRPC_ADDR"))
	if addr == "" {
		addr = fmt.Sprintf("0.0.0.0:%d", PortGRPC)
	}
	return GRPCConfig{Enabled: true, Addr: addr}
}

// NewGRPCServer registers PlasmodAPIService on a new grpc.Server.
func NewGRPCServer(gateway *access.Gateway) *grpc.Server {
	srv := grpc.NewServer()
	plasmodv1.RegisterPlasmodAPIServiceServer(srv, &grpcapi.APIServer{Gateway: gateway})
	reflection.Register(srv)
	return srv
}

// RunGRPC listens and serves until error (other than graceful stop).
func RunGRPC(srv *grpc.Server, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", addr, err)
	}
	log.Printf("Plasmod gRPC listen on %s", addr)
	return srv.Serve(lis)
}
