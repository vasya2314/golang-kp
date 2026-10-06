package movie_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *MovieRepository) PatchMovie(
	ctx context.Context,
	id int,
	movie domain.Movie,
) (domain.Movie, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	UPDATE movies
	SET
		title=$1,
		description=$2,
		release_at=$3,
		version=version+1
	WHERE id=$4 AND version=$5
	RETURNING
		id,
		version,
		title,
		description,
		release_at;
	`

	row := r.pool.QueryRow(ctx, query, movie.Title, movie.Description, movie.ReleaseAt, id, movie.Version)

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
