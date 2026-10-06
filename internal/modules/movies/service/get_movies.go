package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (s *MovieService) GetMovies(
	ctx context.Context,
	limit *int,
	offset *int,
) ([]domain.Movie, error) {
	if limit != nil && *limit < 0 {
		return nil, fmt.Errorf("лимит не может быть отрицательным: %w", core_errors.ErrInvalidArgument)
	}

	if offset != nil && *offset < 0 {
		return nil, fmt.Errorf("смещение не может быть отрицательным: %w", core_errors.ErrInvalidArgument)
	}

	movies, err := s.movieRepository.GetMovies(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("получение списка фильмов: %w", err)
	}

	return movies, nil
}
