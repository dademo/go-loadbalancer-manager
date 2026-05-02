// Package controllers exposes gRPC controllers for the load balancer domain.
package controllers

import (
	"context"
	"errors"
	"fmt"

	loadbalancerv1 "dademo.fr/loadbalancer-manager/internal/gen/proto/loadbalancer/v1"
	"dademo.fr/loadbalancer-manager/internal/services"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HaproxyStatusController exposes HAProxy status and configuration gRPC methods.
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

// GetStatus returns aggregated frontend and backend runtime metrics.
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

// CreateConfiguration validates and creates a managed HAProxy configuration.
func (c *HaproxyStatusController) CreateConfiguration(ctx context.Context, request *loadbalancerv1.CreateHaproxyConfigurationRequest) (*loadbalancerv1.HaproxyConfiguration, error) {
	if request == nil || request.Configuration == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid argument 'configuration': value is required")
	}

	loadBalancing, err := toServiceLoadBalancingStrategy(request.Configuration.LoadBalancingStrategy)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	trafficMode, err := toServiceTrafficMode(request.Configuration.TrafficMode)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	created, err := c.haproxyService.CreateConfiguration(ctx, services.HaproxyConfiguration{
		Name:                request.Configuration.Name,
		FrontendName:        request.Configuration.FrontendName,
		FrontendBindAddress: request.Configuration.FrontendBindAddress,
		FrontendBindPort:    request.Configuration.FrontendBindPort,
		URL:                 request.Configuration.Url,
		LoadBalancing:       loadBalancing,
		BackendName:         request.Configuration.BackendName,
		Backends:            toServiceBackendTargets(request.Configuration.Backends),
		TrafficMode:         trafficMode,
		AutoHTTPSRedirect:   request.Configuration.AutoHttpsRedirect,
		TLS:                 toServiceTLSConfiguration(request.Configuration.Tls),
	})
	if err != nil {
		return nil, mapHaproxyConfigurationError(err)
	}

	return toProtoConfiguration(created), nil
}

// ListConfigurations returns all managed HAProxy configurations.
func (c *HaproxyStatusController) ListConfigurations(ctx context.Context, _ *loadbalancerv1.Empty) (*loadbalancerv1.ListHaproxyConfigurationsResponse, error) {
	configurations := c.haproxyService.ListConfigurations(ctx)

	response := &loadbalancerv1.ListHaproxyConfigurationsResponse{
		Configurations: make([]*loadbalancerv1.HaproxyConfiguration, 0, len(configurations)),
	}

	for _, configuration := range configurations {
		response.Configurations = append(response.Configurations, toProtoConfiguration(configuration))
	}

	return response, nil
}

// GetConfiguration returns a single managed HAProxy configuration by name.
func (c *HaproxyStatusController) GetConfiguration(ctx context.Context, request *loadbalancerv1.GetHaproxyConfigurationRequest) (*loadbalancerv1.HaproxyConfiguration, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid argument 'request': value is required")
	}

	configuration, err := c.haproxyService.GetConfiguration(ctx, request.Name)
	if err != nil {
		return nil, mapHaproxyConfigurationError(err)
	}

	return toProtoConfiguration(configuration), nil
}

// UpdateConfiguration validates and updates an existing managed HAProxy configuration.
func (c *HaproxyStatusController) UpdateConfiguration(ctx context.Context, request *loadbalancerv1.UpdateHaproxyConfigurationRequest) (*loadbalancerv1.HaproxyConfiguration, error) {
	if request == nil || request.Configuration == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid argument 'configuration': value is required")
	}

	loadBalancing, err := toServiceLoadBalancingStrategy(request.Configuration.LoadBalancingStrategy)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	trafficMode, err := toServiceTrafficMode(request.Configuration.TrafficMode)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	updated, err := c.haproxyService.UpdateConfiguration(ctx, services.HaproxyConfiguration{
		Name:                request.Configuration.Name,
		FrontendName:        request.Configuration.FrontendName,
		FrontendBindAddress: request.Configuration.FrontendBindAddress,
		FrontendBindPort:    request.Configuration.FrontendBindPort,
		URL:                 request.Configuration.Url,
		LoadBalancing:       loadBalancing,
		BackendName:         request.Configuration.BackendName,
		Backends:            toServiceBackendTargets(request.Configuration.Backends),
		TrafficMode:         trafficMode,
		AutoHTTPSRedirect:   request.Configuration.AutoHttpsRedirect,
		TLS:                 toServiceTLSConfiguration(request.Configuration.Tls),
	})
	if err != nil {
		return nil, mapHaproxyConfigurationError(err)
	}

	return toProtoConfiguration(updated), nil
}

// DeleteConfiguration deletes a managed HAProxy configuration by name.
func (c *HaproxyStatusController) DeleteConfiguration(ctx context.Context, request *loadbalancerv1.DeleteHaproxyConfigurationRequest) (*loadbalancerv1.Empty, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid argument 'request': value is required")
	}

	if err := c.haproxyService.DeleteConfiguration(ctx, request.Name); err != nil {
		return nil, mapHaproxyConfigurationError(err)
	}

	return &loadbalancerv1.Empty{}, nil
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

func toProtoConfiguration(configuration services.HaproxyConfiguration) *loadbalancerv1.HaproxyConfiguration {
	return &loadbalancerv1.HaproxyConfiguration{
		Name:                  configuration.Name,
		FrontendName:          configuration.FrontendName,
		FrontendBindAddress:   configuration.FrontendBindAddress,
		FrontendBindPort:      configuration.FrontendBindPort,
		Url:                   configuration.URL,
		LoadBalancingStrategy: toProtoLoadBalancingStrategy(configuration.LoadBalancing),
		BackendName:           configuration.BackendName,
		Backends:              toProtoBackendTargets(configuration.Backends),
		TrafficMode:           toProtoTrafficMode(configuration.TrafficMode),
		AutoHttpsRedirect:     configuration.AutoHTTPSRedirect,
		Tls:                   toProtoTLSConfiguration(configuration.TLS),
	}
}

func toServiceTrafficMode(mode loadbalancerv1.HaproxyTrafficMode) (string, error) {
	switch mode {
	case loadbalancerv1.HaproxyTrafficMode_HAPROXY_TRAFFIC_MODE_UNSPECIFIED:
		return "", errors.New("invalid argument 'traffic_mode': value is required")
	case loadbalancerv1.HaproxyTrafficMode_HAPROXY_TRAFFIC_MODE_HTTP:
		return "http", nil
	case loadbalancerv1.HaproxyTrafficMode_HAPROXY_TRAFFIC_MODE_TCP:
		return "tcp", nil
	default:
		return "", fmt.Errorf("invalid argument 'traffic_mode': unsupported enum value %d", mode)
	}
}

func toProtoTrafficMode(mode string) loadbalancerv1.HaproxyTrafficMode {
	switch mode {
	case "http":
		return loadbalancerv1.HaproxyTrafficMode_HAPROXY_TRAFFIC_MODE_HTTP
	case "tcp":
		return loadbalancerv1.HaproxyTrafficMode_HAPROXY_TRAFFIC_MODE_TCP
	default:
		return loadbalancerv1.HaproxyTrafficMode_HAPROXY_TRAFFIC_MODE_UNSPECIFIED
	}
}

func toServiceLoadBalancingStrategy(strategy loadbalancerv1.HaproxyLoadBalancingStrategy) (string, error) {
	switch strategy {
	case loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_UNSPECIFIED:
		return "", errors.New("invalid argument 'load_balancing_strategy': value is required")
	case loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_ROUNDROBIN:
		return "roundrobin", nil
	case loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_LEASTCONN:
		return "leastconn", nil
	case loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_SOURCE:
		return "source", nil
	default:
		return "", fmt.Errorf("invalid argument 'load_balancing_strategy': unsupported enum value %d", strategy)
	}
}

func toProtoLoadBalancingStrategy(strategy string) loadbalancerv1.HaproxyLoadBalancingStrategy {
	switch strategy {
	case "roundrobin":
		return loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_ROUNDROBIN
	case "leastconn":
		return loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_LEASTCONN
	case "source":
		return loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_SOURCE
	default:
		return loadbalancerv1.HaproxyLoadBalancingStrategy_HAPROXY_LOAD_BALANCING_STRATEGY_UNSPECIFIED
	}
}

func toServiceBackendTargets(items []*loadbalancerv1.HaproxyBackendTarget) []services.HaproxyBackendTarget {
	result := make([]services.HaproxyBackendTarget, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		result = append(result, services.HaproxyBackendTarget{
			Name:                item.Name,
			Address:             item.Address,
			Port:                item.Port,
			CheckIntervalSecond: item.CheckIntervalSeconds,
		})
	}

	return result
}

func toProtoBackendTargets(items []services.HaproxyBackendTarget) []*loadbalancerv1.HaproxyBackendTarget {
	result := make([]*loadbalancerv1.HaproxyBackendTarget, 0, len(items))
	for _, item := range items {
		result = append(result, &loadbalancerv1.HaproxyBackendTarget{
			Name:                 item.Name,
			Address:              item.Address,
			Port:                 item.Port,
			CheckIntervalSeconds: item.CheckIntervalSecond,
		})
	}

	return result
}

func toServiceTLSConfiguration(configuration *loadbalancerv1.HaproxyTLSConfiguration) *services.HaproxyTLSConfiguration {
	if configuration == nil {
		return nil
	}

	return &services.HaproxyTLSConfiguration{
		Enabled:              configuration.Enabled,
		CertificatePath:      configuration.CertificatePath,
		PrivateKeyPath:       configuration.PrivateKeyPath,
		CertificatePEM:       configuration.CertificatePem,
		PrivateKeyPEM:        configuration.PrivateKeyPem,
		SkipBackendTLSVerify: configuration.SkipBackendTlsVerify,
	}
}

func toProtoTLSConfiguration(configuration *services.HaproxyTLSConfiguration) *loadbalancerv1.HaproxyTLSConfiguration {
	if configuration == nil {
		return nil
	}

	return &loadbalancerv1.HaproxyTLSConfiguration{
		Enabled:              configuration.Enabled,
		CertificatePath:      configuration.CertificatePath,
		PrivateKeyPath:       configuration.PrivateKeyPath,
		CertificatePem:       configuration.CertificatePEM,
		PrivateKeyPem:        configuration.PrivateKeyPEM,
		SkipBackendTlsVerify: configuration.SkipBackendTLSVerify,
	}
}

func mapHaproxyConfigurationError(err error) error {
	switch {
	case errors.Is(err, services.ErrConfigurationExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, services.ErrConfigurationNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, services.ErrInvalidConfiguration), errors.Is(err, services.ErrInvalidConfigurationKey):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
