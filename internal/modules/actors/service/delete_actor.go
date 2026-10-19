package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	actors_events "github.com/vasya2314/golang-kp/pkg/events/actors"
)

func (s *ActorService) DeleteActor(ctx context.Context, id int) error {
	key, payload, err := actors_events.NewActorDeleted(id)
	if err != nil {
		return err
	}

	event := domain.NewOutboxMessage(actors_events.Topic, key, payload)

	err = s.actorRepository.DeleteActor(ctx, id, event)
	if err != nil {
		return fmt.Errorf("удаление актера/актрисы: %w", err)
	}

	return nil
}
