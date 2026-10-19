package core_kafka

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// envconfig сам разбивает "host1:9092,host2:9092" по запятым
	Brokers []string `envconfig:"BROKERS" required:"true"`
}

func NewConfig() (Config, error) {
	var config Config

	err := envconfig.Process("KAFKA", &config)
	if err != nil {
		return Config{}, fmt.Errorf("обработка переменных окружения: %w", err)
	}

	return config, nil
}

func NewConfigMust() Config {
	config, err := NewConfig()
	if err != nil {
		err = fmt.Errorf("получение конфигурации kafka: %w", err)
		panic(err)
	}

	return config
}
