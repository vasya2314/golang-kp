package movie_postgres_repository

import core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"

type MovieRepository struct {
	pool core_pgx_pool.Pool
}

func NewMovieRepository(pool core_pgx_pool.Pool) *MovieRepository {
	return &MovieRepository{
		pool: pool,
	}
}
