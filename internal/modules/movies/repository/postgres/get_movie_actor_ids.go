package movie_postgres_repository

import (
	"context"
	"fmt"
)

func (r *MovieRepository) GetMovieActorIDs(ctx context.Context, movieID int) ([]int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `SELECT actor_id FROM movie_actors WHERE movie_id = $1;`

	rows, err := r.pool.Query(ctx, query, movieID)
	if err != nil {
		return nil, fmt.Errorf("выборка id актеров/актрис фильма: %w", err)
	}
	defer rows.Close()

	ids := make([]int, 0)

	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("сканирование id актера/актрисы: %w", err)
		}

		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("следующая строка: %w", err)
	}

	return ids, nil
}
