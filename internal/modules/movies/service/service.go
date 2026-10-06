package movie_service

import (
	"context"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type MovieService struct {
	movieRepository MovieRepository
}

type MovieRepository interface {
	CreateMovie(
		ctx context.Context,
		movie domain.Movie,
	) (domain.Movie, error)

	GetMovie(
		ctx context.Context,
		id int,
	) (domain.Movie, error)

	GetMovies(
		ctx context.Context,
		limit *int,
		offset *int,
	) ([]domain.Movie, error)

	DeleteMovie(
		ctx context.Context,
		id int,
	) error
}

func NewMovieService(movieRepository MovieRepository) *MovieService {
	return &MovieService{
		movieRepository: movieRepository,
	}
}
