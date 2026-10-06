package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *MovieService) GetMovie(ctx context.Context, id int) (domain.Movie, error) {
	movie, err := s.movieRepository.GetMovie(ctx, id)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("получение фильма с id %v: %w", id, err)
	}

	return movie, nil
}
