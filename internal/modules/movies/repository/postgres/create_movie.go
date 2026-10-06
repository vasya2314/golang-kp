package movie_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *MovieRepository) CreateMovie(ctx context.Context, movie domain.Movie) (domain.Movie, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	INSERT INTO movies (title, description, release_at)
	VALUES ($1, $2, $3)
	RETURNING id, version, title, description, release_at;
	`

	row := r.pool.QueryRow(ctx, query, movie.Title, movie.Description, movie.ReleaseAt)

	var movieModel MovieModel
	err := row.Scan(
		&movieModel.ID,
		&movieModel.Version,
		&movieModel.Title,
		&movieModel.Description,
		&movieModel.ReleaseAt,
	)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("ошибка чтения результата: %w", err)
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
