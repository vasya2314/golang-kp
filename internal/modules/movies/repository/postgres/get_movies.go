package movie_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *MovieRepository) GetMovies(
	ctx context.Context,
	limit *int,
	offset *int,
) ([]domain.Movie, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `SELECT id, version, title, description, release_at FROM movies ORDER BY id ASC LIMIT $1 OFFSET $2;`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf(`выборка фильмов: %w`, err)
	}
	defer rows.Close()

	var movieModels []MovieModel

	for rows.Next() {
		var movieModel MovieModel

		if err = rows.Scan(
			&movieModel.ID,
			&movieModel.Version,
			&movieModel.Title,
			&movieModel.Description,
			&movieModel.ReleaseAt,
		); err != nil {
			return nil, fmt.Errorf("сканирование фильмов: %w", err)
		}

		movieModels = append(movieModels, movieModel)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("следующая строка: %w", err)
	}

	movieDomains := movieDomainsFromModels(movieModels)

	return movieDomains, nil
}
