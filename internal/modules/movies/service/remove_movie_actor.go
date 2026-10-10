package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) RemoveMovieActor(ctx context.Context, movieID int, actorID int) error {
	err := s.movieRepository.RemoveMovieActor(ctx, movieID, actorID)
	if err != nil {
		return fmt.Errorf("откреплеие актера/актрисы от фильма: %w", err)
	}

	return nil
}
