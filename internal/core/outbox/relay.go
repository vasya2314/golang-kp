package core_outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"
	"go.uber.org/zap"
)

type Relay struct {
	pool      *core_pgx_pool.Pool
	kafka     *kgo.Client
	logger    *core_logger.Logger
	interval  time.Duration
	batchSize int
}

func NewRelay(pool *core_pgx_pool.Pool, kafka *kgo.Client, logger *core_logger.Logger) *Relay {
	return &Relay{
		pool:      pool,
		kafka:     kafka,
		logger:    logger,
		interval:  time.Second,
		batchSize: 100,
	}
}

func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.publishBatch(ctx); err != nil {
				// Не выходим: Kafka или БД могли моргнуть, через секунду попробуем снова
				r.logger.Error("отправка outbox", zap.Error(err))
			}
		}
	}
}

func (r *Relay) publishBatch(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, topic, key, payload
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED;
	`, r.batchSize)
	if err != nil {
		return fmt.Errorf("выборка outbox: %w", err)
	}

	var (
		ids     []int64
		records []*kgo.Record
	)

	for rows.Next() {
		var (
			id         int64
			topic, key string
			payload    []byte
		)
		if err = rows.Scan(&id, &topic, &key, &payload); err != nil {
			rows.Close()

			return fmt.Errorf("сканирование outbox: %w", err)
		}

		ids = append(ids, id)
		records = append(records, &kgo.Record{Topic: topic, Key: []byte(key), Value: payload})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return fmt.Errorf("следующая строка: %w", err)
	}

	if len(records) == 0 {
		return nil
	}

	if err = r.kafka.ProduceSync(ctx, records...).FirstErr(); err != nil {
		// Транзакция откатится, строки останутся неотправленными, следующий тик повторит их.
		// Часть записей могла уже уйти — отсюда дубли (at-least-once)
		return fmt.Errorf("публикация в kafka: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = ANY($1);`, ids)
	if err != nil {
		return fmt.Errorf("пометка outbox: %w", err)
	}

	return tx.Commit(ctx)
}
