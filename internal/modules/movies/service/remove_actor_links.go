package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) RemoveActorLinks(ctx context.Context, actorID int) error {
	if err := s.movieRepository.RemoveActorLinks(ctx, actorID); err != nil {
		return fmt.Errorf("удаление связей актера/актрисы %d с фильмами: %w", actorID, err)
	}

	return nil
}
