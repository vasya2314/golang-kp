package actor_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *ActorRepository) GetActors(
	ctx context.Context,
	limit, offset *int,
) ([]domain.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `SELECT id, version, first_name, last_name, middle_name, description, birth_date FROM actors ORDER BY id ASC LIMIT $1 OFFSET $2;`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("выборка актеров/актрис: %w", err)
	}
	defer rows.Close()

	var actorsModels []ActorModel

	for rows.Next() {
		var actorModel ActorModel

		if err = rows.Scan(
			&actorModel.ID,
			&actorModel.Version,
			&actorModel.FirstName,
			&actorModel.LastName,
			&actorModel.MiddleName,
			&actorModel.Description,
			&actorModel.BirthDate,
		); err != nil {
			return nil, fmt.Errorf("сканирование актеров/актрис: %w", err)
		}

		actorsModels = append(actorsModels, actorModel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("следующая строка: %w", err)
	}

	actorDomains := actorDomainsFromModels(actorsModels)

	return actorDomains, nil
}
