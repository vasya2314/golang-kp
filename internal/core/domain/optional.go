package domain

import (
	"encoding/json"
)

type Optional[T any] struct {
	Set   bool // ключ был в JSON
	Null  bool //  значение было null
	Value T
}

func (o Optional[T]) IsSet() bool  { return o.Set }
func (o Optional[T]) IsNull() bool { return o.Null }
func (o Optional[T]) Any() any     { return &o.Value }

// json вызывает этот метод только если ключ есть в JSON (в том числе для null).
// Если ключа нет, метод не вызывается и Set остаётся false.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Null = true
		return nil
	}

	return json.Unmarshal(data, &o.Value)
}
