package movie_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (r *MovieRepository) GetMovieActors(ctx context.Context, movieId int) ([]domain.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	SELECT a.id, a.version, a.first_name, a.last_name, a.middle_name, a.description, a.birth_date
	FROM movie_actors ma
	JOIN actors a ON a.id = ma.actor_id
	WHERE ma.movie_id = $1
	ORDER BY a.last_name, a.first_name;
	`

	rows, err := r.pool.Query(ctx, query, movieId)
	if err != nil {
		return nil, fmt.Errorf("выборка актеров/актрис фильма: %w", err)
	}
	defer rows.Close()

	actors := make([]domain.Actor, 0)

	for rows.Next() {
		var m movieActorModel

		if err = rows.Scan(
			&m.ID,
			&m.Version,
			&m.FirstName,
			&m.LastName,
			&m.MiddleName,
			&m.Description,
			&m.BirthDate,
		); err != nil {
			return nil, fmt.Errorf("сканирование актеров фильма: %w", err)
		}

		actors = append(actors, domain.NewActor(
			m.ID,
			m.Version,
			m.FirstName,
			m.LastName,
			m.MiddleName,
			m.Description,
			m.BirthDate,
		))
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("следующая строка: %w", err)
	}

	return actors, nil
}
