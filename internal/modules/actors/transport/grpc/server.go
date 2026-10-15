package actor_transport_grpc

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_grpc_server "github.com/vasya2314/golang-kp/internal/core/transport/grpc/server"
	actorsv1 "github.com/vasya2314/golang-kp/pkg/api/actors/v1"
)

const maxBatchSize = 100

type ActorService interface {
	GetActor(ctx context.Context, id int) (domain.Actor, error)
	GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error)
}

type ActorGRPCServer struct {
	// Если в .proto добавят метод, а мы его не реализуем, код всё равно скомпилируется,
	// а метод вернёт codes.Unimplemented
	actorsv1.UnimplementedActorServiceServer

	actorService ActorService
}

func NewActorGRPCServer(actorService ActorService) *ActorGRPCServer {
	return &ActorGRPCServer{actorService: actorService}
}

func (s *ActorGRPCServer) GetActor(
	ctx context.Context,
	req *actorsv1.GetActorRequest,
) (*actorsv1.GetActorResponse, error) {
	actor, err := s.actorService.GetActor(ctx, int(req.GetId()))
	if err != nil {
		return nil, core_grpc_server.ToStatus(err)
	}

	return &actorsv1.GetActorResponse{Actor: actorToProto(actor)}, nil
}

func (s *ActorGRPCServer) BatchGetActors(
	ctx context.Context,
	req *actorsv1.BatchGetActorsRequest,
) (*actorsv1.BatchGetActorsResponse, error) {
	if len(req.GetIds()) > maxBatchSize {
		err := fmt.Errorf("не больше %d id за запрос: %w", maxBatchSize, core_errors.ErrInvalidArgument)

		return nil, core_grpc_server.ToStatus(err)
	}

	ids := make([]int, len(req.GetIds()))
	for i, id := range req.GetIds() {
		ids[i] = int(id)
	}

	actors, err := s.actorService.GetActorsByIDs(ctx, ids)
	if err != nil {
		return nil, core_grpc_server.ToStatus(err)
	}

	response := &actorsv1.BatchGetActorsResponse{
		Actors: make([]*actorsv1.Actor, len(actors)),
	}
	for i, actor := range actors {
		response.Actors[i] = actorToProto(actor)
	}

	return response, nil
}

func actorToProto(actor domain.Actor) *actorsv1.Actor {
	return &actorsv1.Actor{
		Id:          int64(actor.ID),
		Version:     int64(actor.Version),
		FirstName:   actor.FirstName,
		LastName:    actor.LastName,
		MiddleName:  actor.MiddleName,
		Description: actor.Description,
		BirthDate:   actor.BirthDate.Format(domain.DateLayout),
	}
}
