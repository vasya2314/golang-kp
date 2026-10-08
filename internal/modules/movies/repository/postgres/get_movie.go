package movie_postgres_repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *MovieRepository) GetMovie(ctx context.Context, id int) (domain.Movie, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `SELECT id, version, title, description, release_at FROM movies WHERE id = $1`

	row := r.pool.QueryRow(ctx, query, id)

	var movieModel domain.Movie

	err := row.Scan(
		&movieModel.ID,
		&movieModel.Version,
		&movieModel.Title,
		&movieModel.Description,
		&movieModel.ReleaseAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Movie{}, fmt.Errorf("фильм с id %d не найден: %w", id, core_errors.ErrNotFound)
		}

		return domain.Movie{}, fmt.Errorf("ошибка чтения строки: %w", err)
	}

	movieDomain := domain.NewMovie(
		movieModel.ID,
		movieModel.Version,
		movieModel.Title,
		movieModel.Description,
		movieModel.ReleaseAt,
	)

	return movieDomain, nil
}
