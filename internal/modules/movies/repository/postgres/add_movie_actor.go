package movie_postgres_repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

// Код ошибки Postgres «нарушение внешнего ключа»
const pgForeignKeyViolation = "23503"

func (r *MovieRepository) AddMovieActor(ctx context.Context, movieId int, actorId int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	INSERT INTO movie_actors (movie_id, actor_id)
	VALUES ($1, $2)
	ON CONFLICT (movie_id, actor_id) DO NOTHING;
	`

	_, err := r.pool.Exec(ctx, query, movieId, actorId)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return fmt.Errorf(
				"фильм id='%d' или актер/актриса id='%d': %w",
				movieId,
				actorId,
				core_errors.ErrNotFound,
			)
		}

		return fmt.Errorf("выполнение запроса: %w", err)
	}

	return nil
}
