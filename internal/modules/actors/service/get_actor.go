package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *ActorService) GetActor(ctx context.Context, id int) (domain.Actor, error) {
	actor, err := s.actorRepository.GetActor(ctx, id)
	if err != nil {
		return domain.Actor{}, fmt.Errorf("получение актера/актрисы с id %v: %w", id, err)
	}

	return actor, nil
}
