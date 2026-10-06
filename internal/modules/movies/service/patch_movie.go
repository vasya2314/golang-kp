package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *MovieService) PatchMovie(
	ctx context.Context,
	id int,
	patch domain.MoviePatch,
) (domain.Movie, error) {
	movie, err := s.movieRepository.GetMovie(ctx, id)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("репозиторий фильмов: %w", err)
	}

	movie.ApplyPatch(patch)

	patchedMovie, err := s.movieRepository.PatchMovie(ctx, id, movie)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("обновление фильма: %w", err)
	}

	return patchedMovie, nil
}
