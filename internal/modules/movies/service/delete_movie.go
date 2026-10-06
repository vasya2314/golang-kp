package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) DeleteMovie(ctx context.Context, id int) error {
	err := s.movieRepository.DeleteMovie(ctx, id)
	if err != nil {
		return fmt.Errorf("удаление фильма с id %v: %w", id, err)
	}

	return nil
}
