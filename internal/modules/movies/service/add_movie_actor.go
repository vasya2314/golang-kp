package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) AddMovieActor(ctx context.Context, movieId int, actorId int) error {
	// Актёры живут в actors-service: спрашиваем у него.
	// Нет актёра → ErrNotFound → 404; actors-service лежит → ErrServiceUnavailable → 503
	_, err := s.actorProvider.GetActor(ctx, actorId)
	if err != nil {
		return fmt.Errorf("проверка актера/актрисы: %w", err)
	}

	err = s.movieRepository.AddMovieActor(ctx, movieId, actorId)
	if err != nil {
		return fmt.Errorf("привязка актера/актрисы к фильму: %w", err)
	}

	return nil
}
