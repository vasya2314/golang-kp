package domain

type OutboxMessage struct {
	Topic   string
	Key     string
	Payload []byte
}

func NewOutboxMessage(topic string, key string, payload []byte) OutboxMessage {
	return OutboxMessage{
		Topic:   topic,
		Key:     key,
		Payload: payload,
	}
}
