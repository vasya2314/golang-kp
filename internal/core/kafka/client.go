package core_kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// opts — дополнительные настройки. Producer-у (actors) они не нужны,
// а consumer-у (movies) нужно сказать, какую группу и какие топики читать
func NewClient(ctx context.Context, config Config, opts ...kgo.Opt) (*kgo.Client, error) {
	opts = append(opts, kgo.SeedBrokers(config.Brokers...))

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("создание kafka-клиента: %w", err)
	}

	// Как pool.Ping для Postgres: если Kafka недоступна, падаем на старте, а не на первом сообщении
	if err = client.Ping(ctx); err != nil {
		client.Close()

		return nil, fmt.Errorf("проверка соединения с kafka: %w", err)
	}

	return client, nil
}
