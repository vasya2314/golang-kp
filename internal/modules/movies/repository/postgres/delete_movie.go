package movie_postgres_repository

import (
	"context"
	"fmt"

	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *MovieRepository) DeleteMovie(ctx context.Context, id int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `DELETE FROM movies WHERE id = $1;`

	cmdTag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("выполнение запроса: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("фильм с id='%d': %w", id, core_errors.ErrNotFound)
	}

	return nil
}
