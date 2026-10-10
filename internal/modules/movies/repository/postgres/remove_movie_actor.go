package movie_postgres_repository

import (
	"context"
	"fmt"

	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *MovieRepository) RemoveMovieActor(ctx context.Context, movieID, actorID int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `DELETE FROM movie_actors WHERE movie_id = $1 AND actor_id = $2;`

	cmdTag, err := r.pool.Exec(ctx, query, movieID, actorID)
	if err != nil {
		return fmt.Errorf("выполнение запроса: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"связь фильма id='%d' с актером/актрисой id='%d': %w",
			movieID,
			actorID,
			core_errors.ErrNotFound,
		)
	}

	return nil
}
