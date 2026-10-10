package movie_transport_http

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

type MovieActorDTOResponse struct {
	ID          int     `json:"id"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	MiddleName  *string `json:"middle_name"`
	Description *string `json:"description"`
	BirthDate   string  `json:"birth_date"`
}

type GetMovieActorsResponse []MovieActorDTOResponse

func (h *MovieHTTPHandler) GetMovieActors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	idString := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idString)
	if err != nil {
		err = fmt.Errorf("получение `id`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр id")

		return
	}

	actors, err := h.movieService.GetMovieActors(ctx, id)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось получить актеров фильма")

		return
	}

	response := actorsDomainToGetMovieActorsResponse(actors)

	responseHandler.JSONResponse(response, http.StatusOK)
}

func actorsDomainToGetMovieActorsResponse(actors []domain.Actor) []MovieActorDTOResponse {
	result := make([]MovieActorDTOResponse, len(actors))
	for i, actor := range actors {
		result[i] = MovieActorDTOResponse{
			ID:          actor.ID,
			FirstName:   actor.FirstName,
			LastName:    actor.LastName,
			MiddleName:  actor.MiddleName,
			Description: actor.Description,
			BirthDate:   actor.BirthDate.Format(domain.DateLayout),
		}
	}

	return result
}
