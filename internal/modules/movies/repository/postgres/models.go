package movie_postgres_repository

import (
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type MovieModel struct {
	ID          int
	Version     int
	Title       string
	Description *string
	ReleaseAt   time.Time
}

func movieDomainsFromModels(movies []MovieModel) []domain.Movie {
	movieDomains := make([]domain.Movie, len(movies))

	for i, movie := range movies {
		movieDomains[i] = domain.NewMovie(
			movie.ID,
			movie.Version,
			movie.Title,
			movie.Description,
			movie.ReleaseAt,
		)
	}

	return movieDomains
}
