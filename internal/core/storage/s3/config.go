package core_s3_storage

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Endpoint  string        `envconfig:"ENDPOINT" required:"true"`
	Region    string        `envconfig:"REGION" default:"us-east-1"`
	AccessKey string        `envconfig:"ACCESS_KEY" required:"true"`
	SecretKey string        `envconfig:"SECRET_KEY" required:"true"`
	Bucket    string        `envconfig:"BUCKET" required:"true"`
	Timeout   time.Duration `envconfig:"TIMEOUT" default:"30s"`
}

func NewConfig() (Config, error) {
	var config Config

	if err := envconfig.Process("S3", &config); err != nil {
		return Config{}, fmt.Errorf("обработка инициализации хранилища S3: %w", err)
	}

	return config, nil
}

func NewConfigMust() Config {
	config, err := NewConfig()

	if err != nil {
		err = fmt.Errorf("получение конфигурации S3 хранилища: %w", err)
		panic(err)
	}

	return config
}
