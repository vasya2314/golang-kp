package actor_transport_http

import (
	"github.com/vasya2314/golang-kp/internal/core/domain"
)

type ActorDTOResponse struct {
	ID          int     `json:"id"`
	Version     int     `json:"version"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	MiddleName  *string `json:"middle_name"`
	Description *string `json:"description"`
	BirthDate   string  `json:"birth_date"`
}

func actorDTOFromDomain(actor domain.Actor) ActorDTOResponse {
	return ActorDTOResponse{
		ID:          actor.ID,
		Version:     actor.Version,
		FirstName:   actor.FirstName,
		LastName:    actor.LastName,
		MiddleName:  actor.MiddleName,
		Description: actor.Description,
		BirthDate:   actor.BirthDate.Format(domain.DateLayout),
	}
}

func actorDTOFromDomains(actors []domain.Actor) []ActorDTOResponse {
	var actorDTOs = make([]ActorDTOResponse, len(actors))

	for i, actor := range actors {
		actorDTOs[i] = actorDTOFromDomain(actor)
	}

	return actorDTOs
}
