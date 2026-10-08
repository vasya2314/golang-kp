package actor_postgres_repository

import (
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type ActorModel struct {
	ID          int
	Version     int
	FirstName   string
	LastName    string
	MiddleName  *string
	Description *string
	BirthDate   time.Time
}

func actorDomainsFromModels(movies []ActorModel) []domain.Actor {
	actorDomains := make([]domain.Actor, len(movies))

	for i, movie := range movies {
		actorDomains[i] = domain.NewActor(
			movie.ID,
			movie.Version,
			movie.FirstName,
			movie.LastName,
			movie.MiddleName,
			movie.Description,
			movie.BirthDate,
		)
	}

	return actorDomains
}
