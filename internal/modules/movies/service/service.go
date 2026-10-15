package movie_service

import (
	"context"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type MovieService struct {
	movieRepository MovieRepository
	fileStorage     FileStorage
	actorProvider   ActorProvider
}

type ActorProvider interface {
	GetActor(ctx context.Context, id int) (domain.Actor, error)
	GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error)
}

type FileStorage interface {
	Put(ctx context.Context, key string, file domain.File) error
	Get(ctx context.Context, key string) (domain.File, error)
	Delete(ctx context.Context, key string) error
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

	PatchMovie(
		ctx context.Context,
		id int,
		movie domain.Movie,
	) (domain.Movie, error)

	DeleteMovie(
		ctx context.Context,
		id int,
	) error

	AddMovieActor(
		ctx context.Context,
		movieId int,
		actorId int,
	) error

	GetMovieActorIDs(
		ctx context.Context,
		movieID int,
	) ([]int, error)

	RemoveMovieActor(
		ctx context.Context,
		movieID int,
		actorID int,
	) error
}

func NewMovieService(movieRepository MovieRepository, fileStorage FileStorage, actorProvider ActorProvider) *MovieService {
	return &MovieService{
		movieRepository: movieRepository,
		fileStorage:     fileStorage,
		actorProvider:   actorProvider,
	}
}
