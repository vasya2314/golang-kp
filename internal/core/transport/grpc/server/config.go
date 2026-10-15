package core_grpc_server

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Address         string        `envconfig:"ADDRESS" required:"true"`
	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"30s"`
}

func NewConfig() (Config, error) {
	var config Config

	err := envconfig.Process("GRPC", &config)
	if err != nil {
		return Config{}, fmt.Errorf("не удалось обработать переменные окружения: %w", err)
	}

	return config, nil
}

func MustLoadConfig() Config {
	config, err := NewConfig()

	if err != nil {
		err = fmt.Errorf("получение конфигурации GRPC-сервера: %w", err)
		panic(err)
	}

	return config
}
