package actor_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *ActorRepository) PatchActor(
	ctx context.Context,
	id int,
	actor domain.Actor,
) (domain.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	UPDATE actors
	SET
		first_name = $1,
		last_name = $2,
		middle_name = $3,
		description = $4,
		birth_date = $5,
		version=version+1
	WHERE id=$6 AND version=$7
	RETURNING
		id,
		version,
		first_name,
		last_name,
		middle_name,
		description,
		birth_date;
	`

	row := r.pool.QueryRow(ctx, query, actor.FirstName, actor.LastName, actor.MiddleName, actor.Description, actor.BirthDate, id, actor.Version)

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
