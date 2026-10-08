package actor_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *ActorRepository) CreateActor(
	ctx context.Context,
	actor domain.Actor,
) (domain.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
		INSERT INTO actors (first_name, last_name, middle_name, description, birth_date)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, version, first_name, last_name, middle_name, description, birth_date;
	`

	row := r.pool.QueryRow(ctx, query, actor.FirstName, actor.LastName, actor.MiddleName, actor.Description, actor.BirthDate)

	var actorModel ActorModel
	err := row.Scan(
		&actorModel.ID,
		&actorModel.Version,
		&actorModel.FirstName,
		&actorModel.LastName,
		&actorModel.MiddleName,
		&actorModel.Description,
		&actorModel.BirthDate,
	)

	if err != nil {
		return domain.Actor{}, fmt.Errorf("ошибка чтения результата: %w", err)
	}

	actorDomain := domain.NewActor(
		actorModel.ID,
		actorModel.Version,
		actorModel.FirstName,
		actorModel.LastName,
		actorModel.MiddleName,
		actorModel.Description,
		actorModel.BirthDate,
	)

	return actorDomain, nil
}
