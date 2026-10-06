package core_pgx_pool

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Host     string        `envconfig:"HOST" required:"true"`
	Port     string        `envconfig:"PORT" default:"5432"`
	User     string        `envconfig:"USER" required:"true"`
	Password string        `envconfig:"PASSWORD" required:"true"`
	Database string        `envconfig:"DB" required:"true"`
	Timeout  time.Duration `envconfig:"TIMEOUT" default:"5m"`
}

func NewConfig() (Config, error) {
	var config Config

	err := envconfig.Process("POSTGRES", &config)
	if err != nil {
		return Config{}, fmt.Errorf("обработка переменных окружения: %w", err)
	}

	return config, nil
}

func NewConfigMust() Config {
	config, err := NewConfig()
	if err != nil {
		err = fmt.Errorf("получение конфигурации пула подключений postgres: %w", err)
		panic(err)
	}

	return config
}
