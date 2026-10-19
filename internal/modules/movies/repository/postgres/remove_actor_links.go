package movie_postgres_repository

import (
	"context"
	"fmt"
)

func (r *MovieRepository) RemoveActorLinks(ctx context.Context, actorID int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `DELETE FROM movie_actors WHERE actor_id = $1;`

	if _, err := r.pool.Exec(ctx, query, actorID); err != nil {
		return fmt.Errorf("выполнение запроса: %w", err)
	}

	return nil
}
