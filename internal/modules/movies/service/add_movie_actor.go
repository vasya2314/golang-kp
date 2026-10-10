package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) AddMovieActor(ctx context.Context, movieId int, actorId int) error {
	err := s.movieRepository.AddMovieActor(ctx, movieId, actorId)
	if err != nil {
		return fmt.Errorf("привязка актера/актрисы к фильму: %w", err)
	}

	return nil
}
