package services

import "google.golang.org/grpc"

// GrpcServerOptionsProvider provides one gRPC server option for Fx group wiring.
type GrpcServerOptionsProvider interface {
	GetOption() (grpc.ServerOption, error)
}
