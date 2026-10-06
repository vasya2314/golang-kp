package movie_transport_http

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type MovieHTTPHandler struct {
	movieService MovieService
}

type MovieService interface {
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
		moviePatch domain.MoviePatch,
	) (domain.Movie, error)

	DeleteMovie(
		ctx context.Context,
		id int,
	) error
}

func NewMovieHTTPHandler(s MovieService) *MovieHTTPHandler {
	return &MovieHTTPHandler{
		movieService: s,
	}
}

func (h *MovieHTTPHandler) Routes(router *chi.Mux) {
	router.Route("/movies", func(r chi.Router) {
		r.Post("/", h.CreateMovie)
		r.Get("/", h.GetMovies)
		r.Delete("/{id}", h.DeleteMovie)
		r.Get("/{id}", h.GetMovie)
		r.Patch("/{id}", h.PatchMovie)
	})
}
