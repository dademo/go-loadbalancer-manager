package services

import "google.golang.org/grpc"

type GrpcServerOptionsProvider interface {
	GetOption() (grpc.ServerOption, error)
}
