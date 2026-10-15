package movie_actors_client

import (
	"context"
	"fmt"
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_grpc_server "github.com/vasya2314/golang-kp/internal/core/transport/grpc/server"
	actorsv1 "github.com/vasya2314/golang-kp/pkg/api/actors/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Таймаут и ретраи описываются декларативно, и gRPC применяет их сам.
// Ретраим только UNAVAILABLE (сервис не ответил вообще) — оба метода только читают, повтор безопасен
const serviceConfig = `{
  "methodConfig": [{
    "name": [{"service": "actors.v1.ActorService"}],
    "timeout": "2s",
    "retryPolicy": {
      "maxAttempts": 3,
      "initialBackoff": "0.1s",
      "maxBackoff": "1s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

type ActorsClient struct {
	conn   *grpc.ClientConn
	client actorsv1.ActorServiceClient
}

func NewActorsClient(config Config) (*ActorsClient, error) {
	// NewClient не подключается сразу: соединение установится при первом вызове
	// и будет переиспользоваться всеми запросами (HTTP/2, мультиплексирование)
	conn, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("создание gRPC-клиента actors: %w", err)
	}

	return &ActorsClient{
		conn:   conn,
		client: actorsv1.NewActorServiceClient(conn),
	}, nil
}

func (c *ActorsClient) Close() error {
	return c.conn.Close()
}

func (c *ActorsClient) GetActor(ctx context.Context, id int) (domain.Actor, error) {
	resp, err := c.client.GetActor(ctx, &actorsv1.GetActorRequest{Id: int64(id)})
	if err != nil {
		return domain.Actor{}, fmt.Errorf("actors.GetActor: %w", core_grpc_server.FromStatus(err))
	}

	return actorFromProto(resp.GetActor())
}

func (c *ActorsClient) GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error) {
	protoIDs := make([]int64, len(ids))
	for i, id := range ids {
		protoIDs[i] = int64(id)
	}

	resp, err := c.client.BatchGetActors(ctx, &actorsv1.BatchGetActorsRequest{Ids: protoIDs})
	if err != nil {
		return nil, fmt.Errorf("actors.BatchGetActors: %w", core_grpc_server.FromStatus(err))
	}

	actors := make([]domain.Actor, 0, len(resp.GetActors()))
	for _, protoActor := range resp.GetActors() {
		actor, err := actorFromProto(protoActor)
		if err != nil {
			return nil, err
		}

		actors = append(actors, actor)
	}

	return actors, nil
}

func actorFromProto(actor *actorsv1.Actor) (domain.Actor, error) {
	// Геттеры не паникуют на nil, но пустой актёр нам всё равно не нужен
	if actor == nil {
		return domain.Actor{}, fmt.Errorf("в ответе actors-service нет актера: %w", core_errors.ErrInternalServerError)
	}

	date, err := time.Parse(domain.DateLayout, actor.GetBirthDate())
	if err != nil {
		return domain.Actor{}, fmt.Errorf("парсинг даты рождения актера %d: %w", actor.GetId(), err)
	}

	return domain.NewActor(
		int(actor.GetId()),
		int(actor.GetVersion()),
		actor.GetFirstName(),
		actor.GetLastName(),
		actor.MiddleName,
		actor.Description,
		date,
	), nil
}
