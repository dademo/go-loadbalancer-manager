package controllers

import (
	"context"

	loadbalancerv1 "dademo.fr/loadbalancer-manager/internal/gen/proto/loadbalancer/v1"
	"dademo.fr/loadbalancer-manager/internal/services"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

type HaproxyStatusController struct {
	loadbalancerv1.UnimplementedHaproxyStatusServiceServer
	logger         zerolog.Logger
	haproxyService *services.HaproxyService
}

func newHaproxyStatusController(
	logger zerolog.Logger,
	haproxyService *services.HaproxyService,
	grpcServer *services.GrpcServerService,
) *HaproxyStatusController {
	controller := &HaproxyStatusController{
		logger:         logger.With().Str("component", "haproxy_status_controller").Logger(),
		haproxyService: haproxyService,
	}

	grpcServer.RegisterGrpcService(func(registrar grpc.ServiceRegistrar) {
		loadbalancerv1.RegisterHaproxyStatusServiceServer(registrar, controller)
	})

	controller.logger.Info().Str("service", "loadbalancer.v1.HaproxyStatusService").Msg("HAProxy status gRPC controller registered")
	return controller
}

func (c *HaproxyStatusController) GetStatus(ctx context.Context, _ *loadbalancerv1.Empty) (*loadbalancerv1.HaproxyStatusResponse, error) {
	status, err := c.haproxyService.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	response := &loadbalancerv1.HaproxyStatusResponse{
		Frontends: toProtoStatusList(status.Frontends),
		Backends:  toProtoStatusList(status.Backends),
	}

	return response, nil
}

func toProtoStatusList(items []services.HaproxyProxyStatus) []*loadbalancerv1.HaproxyProxyStatus {
	result := make([]*loadbalancerv1.HaproxyProxyStatus, 0, len(items))
	for _, item := range items {
		result = append(result, &loadbalancerv1.HaproxyProxyStatus{
			Name:       item.Name,
			Status:     item.Status,
			Current:    item.Current,
			Max:        item.Max,
			Total:      item.Total,
			Rate:       item.Rate,
			BytesIn:    item.BytesIn,
			BytesOut:   item.BytesOut,
			LastChange: item.LastChange,
		})
	}

	return result
}
