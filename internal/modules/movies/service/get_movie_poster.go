package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (s *MovieService) GetMoviePoster(
	ctx context.Context,
	movieId int,
) (domain.File, error) {
	movie, err := s.movieRepository.GetMovie(ctx, movieId)
	if err != nil {
		return domain.File{}, fmt.Errorf("получение фильма: %w", err)
	}

	if movie.PosterKey == nil {
		return domain.File{}, fmt.Errorf("у фильма id='%d' нет постера: %w", movieId, core_errors.ErrNotFound)
	}

	file, err := s.fileStorage.Get(ctx, *movie.PosterKey)
	if err != nil {
		return domain.File{}, fmt.Errorf("получение файла из хранилища: %w", err)
	}

	return file, nil
}
