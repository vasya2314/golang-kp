package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (s *ActorService) GetActors(ctx context.Context, limit, offset *int) ([]domain.Actor, error) {
	if limit != nil && *limit < 0 {
		return nil, fmt.Errorf("лимит не может быть отрицательным: %w", core_errors.ErrInvalidArgument)
	}

	if offset != nil && *offset < 0 {
		return nil, fmt.Errorf("смещение не может быть отрицательным: %w", core_errors.ErrInvalidArgument)
	}

	actors, err := s.actorRepository.GetActors(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("получение списка акторов/актрис: %w", err)
	}

	return actors, nil
}
