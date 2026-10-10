package movie_transport_http

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

func (h *MovieHTTPHandler) AddMovieActor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	movieIdString := chi.URLParam(r, "id")

	movieId, err := strconv.Atoi(movieIdString)
	if err != nil {
		err = fmt.Errorf("получение `id`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр id")

		return
	}

	actorIdString := chi.URLParam(r, "actorId")

	actorId, err := strconv.Atoi(actorIdString)
	if err != nil {
		err = fmt.Errorf("получение `actorId`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр actorId")

		return
	}

	err = h.movieService.AddMovieActor(ctx, movieId, actorId)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось привязать актера/актрису к фильму")

		return
	}

	responseHandler.NoContentResponse()
}
