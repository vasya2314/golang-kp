package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *MovieService) CreateMovie(ctx context.Context, movie domain.Movie) (domain.Movie, error) {
	createMovie, err := s.movieRepository.CreateMovie(ctx, movie)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("создание фильма: %w", err)
	}

	return createMovie, nil
}
