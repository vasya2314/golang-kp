package actor_postgres_repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *ActorRepository) GetActor(ctx context.Context, id int) (domain.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `SELECT id, version, first_name, last_name, middle_name, description, birth_date FROM actors WHERE id = $1;`

	row := r.pool.QueryRow(ctx, query, id)

	var actorModel ActorModel

	err := row.Scan(
		&actorModel.ID,
		&actorModel.Version,
		&actorModel.FirstName,
		&actorModel.LastName,
		&actorModel.MiddleName,
		&actorModel.Description,
		&actorModel.BirthDate)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Actor{}, fmt.Errorf("актер/актриса с id %d не найден: %w", id, core_errors.ErrNotFound)
		}

		return domain.Actor{}, fmt.Errorf("ошибка чтения строки: %w", err)
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
