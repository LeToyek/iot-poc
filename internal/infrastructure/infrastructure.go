package infrastructure

import "iot-poc/internal/infrastructure/configuration"

type infrastructure struct {
	configuration.Config
}

func NewInfrastructure(config configuration.Config) *infrastructure {
	return &infrastructure{
		config,
	}
}
