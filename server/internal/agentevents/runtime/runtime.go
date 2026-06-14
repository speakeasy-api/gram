package runtime

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	chatmessagesink "github.com/speakeasy-api/gram/server/internal/agentevents/eventsink/sinks/chatmessage"
	telemetrysink "github.com/speakeasy-api/gram/server/internal/agentevents/eventsink/sinks/telemetry"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

type Config struct {
	DB                 hooksRepo.DBTX
	TelemetryLogger    telemetrysink.Logger
	ChatWriter         chatmessagesink.Writer
	ProductFeatures    chatmessagesink.ProductFeaturesClient
	ChatTitleGenerator chatmessagesink.TitleGenerator
}

type ProviderRegistration interface {
	register(*agentevents.Mux, Config) error
}

type providerRegistration[T any] struct {
	spec agentevents.Spec[T]
}

func Provider[T any](spec agentevents.Spec[T]) ProviderRegistration {
	return providerRegistration[T]{spec: spec}
}

func (p providerRegistration[T]) register(mux *agentevents.Mux, config Config) error {
	return registerStandardProvider(mux, p.spec, config)
}

func New(config Config, providers ...ProviderRegistration) (*agentevents.Mux, error) {
	mux := agentevents.NewMux()

	for _, provider := range providers {
		if provider == nil {
			return nil, fmt.Errorf("agentevents runtime: nil provider registration")
		}
		if err := provider.register(mux, config); err != nil {
			return nil, err
		}
	}

	return mux, nil
}

func registerStandardProvider[T any](mux *agentevents.Mux, spec agentevents.Spec[T], config Config) error {
	return registerProvider(mux, spec,
		telemetrysink.Installer[T](config.TelemetryLogger),
		chatmessagesink.Installer[T](chatmessagesink.Config{
			Writer:          config.ChatWriter,
			ProductFeatures: config.ProductFeatures,
			DB:              config.DB,
			TitleGenerator:  config.ChatTitleGenerator,
		}),
	)
}

func registerProvider[T any](mux *agentevents.Mux, spec agentevents.Spec[T], installers ...agentevents.SinkInstaller[T]) error {
	agent, err := spec.Agent()
	if err != nil {
		return err
	}

	for _, installer := range installers {
		if installer == nil {
			return fmt.Errorf("agentevents runtime: nil sink installer for provider %s", spec.Provider)
		}
		if err := installer.Install(agent); err != nil {
			return err
		}
	}

	return mux.Register(agent, nil)
}
