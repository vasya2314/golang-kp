package actor_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *ActorRepository) DeleteActor(ctx context.Context, id int, event domain.OutboxMessage) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	// открывает транзакцию
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	// страховка отката изменений
	defer tx.Rollback(ctx)

	query := `DELETE FROM actors WHERE id = $1;`

	cmdTag, err := tx.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("выполнение запроса: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("актер/актриса с id='%d': %w", id, core_errors.ErrNotFound)
	}

	// кладем запись в таблицу outbox
	_, err = tx.Exec(
		ctx,
		`INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3);`,
		event.Topic,
		event.Key,
		event.Payload,
	)
	if err != nil {
		return fmt.Errorf("запись события в outbox: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("фиксация транзакц: %w", err)
	}

	return nil
}
