package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *MovieService) GetMovieActors(
	ctx context.Context,
	movieId int,
) ([]domain.Actor, error) {
	_, err := s.movieRepository.GetMovie(ctx, movieId)
	if err != nil {
		return nil, fmt.Errorf("получение фильма: %w", err)
	}

	actors, err := s.movieRepository.GetMovieActors(ctx, movieId)
	if err != nil {
		return nil, fmt.Errorf("получение актеров/актрис фильма: %w", err)
	}

	return actors, nil
}
