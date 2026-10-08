package actor_postgres_repository

import core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"

type ActorRepository struct {
	pool core_pgx_pool.Pool
}

func NewActorRepository(pool core_pgx_pool.Pool) *ActorRepository {
	return &ActorRepository{
		pool: pool,
	}
}
