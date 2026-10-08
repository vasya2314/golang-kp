package actor_transport_http

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

func (h *ActorHTTPHandler) DeleteActor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	idString := chi.URLParam(r, "id")

	id, err := strconv.Atoi(idString)
	if err != nil {
		responseHandler.ErrorResponse(err, "некорректный параметр id")

		return
	}

	err = h.actorService.DeleteActor(ctx, id)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось удалить актера/актрису")

		return
	}

	responseHandler.NoContentResponse()
}
