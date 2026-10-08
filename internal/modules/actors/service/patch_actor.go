package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *ActorService) PatchActor(
	ctx context.Context,
	id int,
	patch domain.ActorPatch,
) (domain.Actor, error) {
	actor, err := s.actorRepository.GetActor(ctx, id)
	if err != nil {
		return domain.Actor{}, fmt.Errorf("репозиторий актеров/актрис: %w", err)
	}

	actor.ApplyPatch(patch)

	patchedMovie, err := s.actorRepository.PatchActor(ctx, id, actor)
	if err != nil {
		return domain.Actor{}, fmt.Errorf("обновление актеров/актрис: %w", err)
	}

	return patchedMovie, nil
}
