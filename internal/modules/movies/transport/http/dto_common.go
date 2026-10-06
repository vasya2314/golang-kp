package movie_transport_http

import (
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type MovieDTOResponse struct {
	ID          int       `json:"id"`
	Version     int       `json:"version"`
	Title       string    `json:"title"`
	Description *string   `json:"description"`
	ReleaseAt   time.Time `json:"release_at"`
}

func movieDTOFromDomain(movie domain.Movie) MovieDTOResponse {
	return MovieDTOResponse{
		ID:          movie.ID,
		Version:     movie.Version,
		Title:       movie.Title,
		Description: movie.Description,
		ReleaseAt:   movie.ReleaseAt,
	}
}

func movieDTOFromDomains(movies []domain.Movie) []MovieDTOResponse {
	var movieDTOs = make([]MovieDTOResponse, len(movies))

	for i, movie := range movies {
		movieDTOs[i] = movieDTOFromDomain(movie)
	}

	return movieDTOs
}
