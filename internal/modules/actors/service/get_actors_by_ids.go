package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *ActorService) GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error) {
	actors, err := s.actorRepository.GetActorsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("получение актеров/актрис по id: %w", err)
	}

	return actors, nil
}
