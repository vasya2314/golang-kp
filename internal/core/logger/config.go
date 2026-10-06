package core_logger

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Level  string `envconfig:"LEVEL" default:"DEBUG"`
	Folder string `envconfig:"FOLDER" required:"true"`
}

func NewConfig() (Config, error) {
	var cfg Config

	err := envconfig.Process("LOGGER", &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("ошибка обработки конфигурации: %w", err)
	}

	return cfg, nil
}

func NewConfigMust() Config {
	cfg, err := NewConfig()
	if err != nil {
		err = fmt.Errorf("ошибка загрузки конфигурации: %w", err)
		panic(err)
	}

	return cfg
}
