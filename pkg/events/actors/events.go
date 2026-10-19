package actors_events

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	Topic = "actors.events"

	TypeActorDeleted = "actor.deleted"
)

// Envelope — общая обёртка для всех событий топика.
// Потребитель сначала читает Type, потом разбирает Payload в нужную структуру
type Envelope struct {
	EventID    string          `json:"event_id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

type ActorDeleted struct {
	ActorID int `json:"actor_id"`
}

// NewActorDeleted возвращает ключ и значение сообщения Kafka
func NewActorDeleted(actorID int) (string, []byte, error) {
	payload, err := json.Marshal(ActorDeleted{ActorID: actorID})
	if err != nil {
		return "", nil, fmt.Errorf("сериализация payload: %w", err)
	}

	value, err := json.Marshal(Envelope{
		EventID:    uuid.NewString(),
		Type:       TypeActorDeleted,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
	if err != nil {
		return "", nil, fmt.Errorf("сериализация события: %w", err)
	}

	return strconv.Itoa(actorID), value, nil
}
