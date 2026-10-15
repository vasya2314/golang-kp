package actor_transport_http

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type ActorHTTPHandler struct {
	actorService ActorService
}

type ActorService interface {
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
		actorPatch domain.ActorPatch,
	) (domain.Actor, error)

	DeleteActor(
		ctx context.Context,
		id int,
	) error
}

func NewActorHTTPHandler(actorService ActorService) *ActorHTTPHandler {
	return &ActorHTTPHandler{
		actorService: actorService,
	}
}

func (h *ActorHTTPHandler) Routes(router *chi.Mux) {
	router.Route("/actors", func(r chi.Router) {
		r.Post("/", h.CreateActor)
		r.Get("/", h.GetActors)
		r.Delete("/{id}", h.DeleteActor)
		r.Get("/{id}", h.GetActor)
		r.Patch("/{id}", h.PatchActor)
	})
}
