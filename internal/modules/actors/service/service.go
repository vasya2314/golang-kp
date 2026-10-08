package actor_service

import (
	"context"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type ActorService struct {
	actorRepository ActorRepository
}

type ActorRepository interface {
	CreateActor(
		ctx context.Context,
		actor domain.Actor,
	) (domain.Actor, error)

	GetActor(
		ctx context.Context,
		id int,
	) (domain.Actor, error)

	GetActors(
		ctx context.Context,
		limit, offset *int,
	) ([]domain.Actor, error)

	PatchActor(
		ctx context.Context,
		id int,
		actor domain.Actor,
	) (domain.Actor, error)

	DeleteActor(
		ctx context.Context,
		id int,
	) error
}

func NewActorService(actorRepository ActorRepository) *ActorService {
	return &ActorService{
		actorRepository: actorRepository,
	}
}
