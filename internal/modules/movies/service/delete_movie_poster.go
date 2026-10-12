package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) DeleteMoviePoster(ctx context.Context, id int) error {
	movie, err := s.movieRepository.GetMovie(ctx, id)
	if err != nil {
		return fmt.Errorf("удаление постера: %w", err)
	}

	if movie.PosterKey != nil {
		err = s.fileStorage.Delete(ctx, *movie.PosterKey)

		if err != nil {
			return fmt.Errorf("удаление файла: %w", err)
		}
	}

	return nil
}
