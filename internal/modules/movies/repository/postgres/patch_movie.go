package movie_postgres_repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
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
		poster_key=$4,
		version=version+1
	WHERE id=$5 AND version=$6
	RETURNING
		id,
		version,
		title,
		description,
		release_at,
		poster_key;
	`

	row := r.pool.QueryRow(ctx, query, movie.Title, movie.Description, movie.ReleaseAt, movie.PosterKey, id, movie.Version)

	var movieModel MovieModel
	err := row.Scan(
		&movieModel.ID,
		&movieModel.Version,
		&movieModel.Title,
		&movieModel.Description,
		&movieModel.ReleaseAt,
		&movieModel.PosterKey,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Movie{}, fmt.Errorf(
				"фильм с id='%d' конкурентный доступ: %w",
				id,
				core_errors.ErrConflict,
			)
		}

		return domain.Movie{}, fmt.Errorf("ошибка чтения результата: %w", err)
	}

	movieDomain := domain.NewMovie(
		movieModel.ID,
		movieModel.Version,
		movieModel.Title,
		movieModel.Description,
		movieModel.ReleaseAt,
		movieModel.PosterKey,
	)

	return movieDomain, nil
}
