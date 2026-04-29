package services

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

// EndpointStats stores execution data for a specific gRPC method
type EndpointStats struct {
	TotalCalls  int64
	TotalTime   time.Duration
	AverageTime time.Duration
}

type MetricsService struct {
	logger zerolog.Logger
	mu     sync.RWMutex
	stats  map[string]*EndpointStats
}

func newMetricsService(logger zerolog.Logger) MetricsService {
	return MetricsService{
		logger: logger.With().Str("component", "metrics_service").Logger(),
		mu:     sync.RWMutex{},
		stats:  make(map[string]*EndpointStats),
	}
}

func (m *MetricsService) GetGrpcServerOption() grpc.ServerOption {
	return grpc.UnaryInterceptor(m.onGrpcRequestReceived)
}

// grpc.UnaryServerInterceptor
func (m *MetricsService) onGrpcRequestReceived(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (resp any, err error) {

	start := time.Now()

	// Execute the actual RPC call
	resp, err = handler(ctx, req)

	// Record metrics (Duration, Success/Failure, etc.)
	duration := time.Since(start)

	m.logger.Info().
		Str("method", info.FullMethod).
		Dur("duration", duration).
		Bool("success", err == nil).
		Msg("RPC Call Stats")

	return resp, err
}
