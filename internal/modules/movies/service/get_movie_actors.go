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
	// Без этой проверки для несуществующего фильма вернётся [] вместо 404
	_, err := s.movieRepository.GetMovie(ctx, movieId)
	if err != nil {
		return nil, fmt.Errorf("получение фильма: %w", err)
	}

	actorIDs, err := s.movieRepository.GetMovieActorIDs(ctx, movieId)
	if err != nil {
		return nil, fmt.Errorf("получение id актеров/актрис фильма: %w", err)
	}

	// У фильма нет актёров — не ходим по сети зря
	if len(actorIDs) == 0 {
		return []domain.Actor{}, nil
	}

	actors, err := s.actorProvider.GetActorsByIDs(ctx, actorIDs)
	if err != nil {
		return nil, fmt.Errorf("получение актеров/актрис фильма: %w", err)
	}

	return actors, nil
}
