package actor_service

import (
	"context"
	"fmt"
)

func (s *ActorService) DeleteActor(ctx context.Context, id int) error {
	err := s.actorRepository.DeleteActor(ctx, id)
	if err != nil {
		return fmt.Errorf("удаление актера/актрисы: %w", err)
	}

	return nil
}
