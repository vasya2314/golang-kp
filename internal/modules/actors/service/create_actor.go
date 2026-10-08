package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *ActorService) CreateActor(
	ctx context.Context,
	actor domain.Actor,
) (domain.Actor, error) {
	createActor, err := s.actorRepository.CreateActor(ctx, actor)
	if err != nil {
		return domain.Actor{}, fmt.Errorf("создание актера/актрисы: %w", err)
	}

	return createActor, nil
}
