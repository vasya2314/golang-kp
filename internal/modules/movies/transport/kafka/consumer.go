package movie_transport_kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	actors_events "github.com/vasya2314/golang-kp/pkg/events/actors"
	"go.uber.org/zap"
)

const (
	ConsumerGroup = "movies-service"
	maxBackoff    = 10 * time.Second
)

type MovieService interface {
	RemoveActorLinks(ctx context.Context, actorID int) error
}

type ActorEventsConsumer struct {
	client       *kgo.Client
	movieService MovieService
	logger       *core_logger.Logger
}

func NewActorEventsConsumer(
	client *kgo.Client,
	movieService MovieService,
	logger *core_logger.Logger,
) *ActorEventsConsumer {
	return &ActorEventsConsumer{client: client, movieService: movieService, logger: logger}
}

func (c *ActorEventsConsumer) Run(ctx context.Context) error {
	for {
		// Ждёт новые сообщения. Возвращает пачку (может быть из нескольких партиций)
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			c.logger.Error(
				"ошибка чтения из kafka",
				zap.String("topic", topic),
				zap.Int32("partition", partition),
				zap.Error(err),
			)
		})

		fetches.EachRecord(func(record *kgo.Record) {
			c.handleWithRetry(ctx, record)
		})

		// Если нас остановили посреди пачки, не коммитим: необработанное придёт снова после рестарта
		if ctx.Err() != nil {
			return nil
		}

		// «Закладка»: всё, что прочитали в этой пачке, обработано
		if err := c.client.CommitUncommittedOffsets(ctx); err != nil {
			c.logger.Error("коммит offset-ов", zap.Error(err))
		}
	}
}

// Повторяем, пока не получится: временная ошибка (БД моргнула) не должна терять событие
func (c *ActorEventsConsumer) handleWithRetry(ctx context.Context, record *kgo.Record) {
	backoff := 100 * time.Millisecond

	for {
		err := c.handle(ctx, record)
		if err == nil || ctx.Err() != nil {
			return
		}

		c.logger.Warn(
			"обработка события, повтор",
			zap.Int64("offset", record.Offset),
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *ActorEventsConsumer) handle(ctx context.Context, record *kgo.Record) error {
	var envelope actors_events.Envelope
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		// Битое сообщение не починится повтором. Если ретраить — застрянем на нём навсегда
		c.logger.Error("невалидное сообщение, пропускаем", zap.Int64("offset", record.Offset), zap.Error(err))

		return nil
	}

	log := c.logger.With(
		zap.String("event_id", envelope.EventID),
		zap.String("event_type", envelope.Type),
	)
	ctx = core_logger.ToContext(ctx, log)

	switch envelope.Type {
	case actors_events.TypeActorDeleted:
		var payload actors_events.ActorDeleted
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			log.Error("невалидный payload, пропускаем", zap.Error(err))

			return nil
		}

		if err := c.movieService.RemoveActorLinks(ctx, payload.ActorID); err != nil {
			return fmt.Errorf("удаление связей актера %d: %w", payload.ActorID, err)
		}

		log.Debug("связи актера удалены", zap.Int("actor_id", payload.ActorID))

	default:
		// Незнакомый тип — не ошибка: actors-service мог добавить новое событие,
		// которое нам пока не интересно. Просто пропускаем
	}

	return nil
}
