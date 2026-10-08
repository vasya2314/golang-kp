package actor_transport_http

import (
	"net/http"
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_request "github.com/vasya2314/golang-kp/internal/core/transport/http/request"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

type CreateActorRequest struct {
	FirstName   string  `json:"first_name" validate:"required,max=100"`
	LastName    string  `json:"last_name" validate:"required,max=100"`
	MiddleName  *string `json:"middle_name" validate:"omitempty,max=100"`
	Description *string `json:"description" validate:"omitempty,max=1000"`
	BirthDate   string  `json:"birth_date" validate:"required,datetime=2006-01-02"`
}

type CreateActorResponse ActorDTOResponse

func (h *ActorHTTPHandler) CreateActor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	var req CreateActorRequest
	err := core_http_request.DecodeAndValidateRequest(r, &req)
	if err != nil {
		responseHandler.ErrorResponse(err, "ошибка валидации")

		return
	}

	btDate, err := time.Parse(domain.DateLayout, req.BirthDate)
	if err != nil {
		responseHandler.ErrorResponse(err, "дата указана в неверном формате")

		return
	}

	actorDomain := domain.NewActorUninitialized(req.FirstName, req.LastName, req.MiddleName, req.Description, btDate)

	actorDomain, err = h.actorService.CreateActor(ctx, actorDomain)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось создать актера/актрису")

		return
	}

	response := CreateActorResponse(actorDTOFromDomain(actorDomain))

	responseHandler.JSONResponse(response, http.StatusCreated)
}
