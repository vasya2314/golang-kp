package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) DeleteMovie(ctx context.Context, id int) error {
	movie, err := s.movieRepository.GetMovie(ctx, id)
	if err != nil {
		return fmt.Errorf("получение фильма: %w", err)
	}

	err = s.movieRepository.DeleteMovie(ctx, id)
	if err != nil {
		return fmt.Errorf("удаление фильма с id %v: %w", id, err)
	}

	if movie.PosterKey != nil {
		s.deletePosterQuietly(ctx, *movie.PosterKey)
	}

	return nil
}
