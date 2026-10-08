package actor_transport_http

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_request "github.com/vasya2314/golang-kp/internal/core/transport/http/request"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

type PatchActorRequest struct {
	FirstName   domain.Optional[string] `json:"first_name" validate:"omitempty,notnull,min=1,max=100"`
	LastName    domain.Optional[string] `json:"last_name" validate:"omitempty,notnull,min=1,max=100"`
	MiddleName  domain.Optional[string] `json:"middle_name" validate:"omitempty,null|min=1,max=100"`
	Description domain.Optional[string] `json:"description" validate:"omitempty,null|min=1,max=1000"`
	BirthDate   domain.Optional[string] `json:"birth_date" validate:"omitempty,notnull,datetime=2006-01-02"`
}

type PatchActorResponse ActorDTOResponse

func (h *ActorHTTPHandler) PatchActor(w http.ResponseWriter, r *http.Request) {
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

	var req PatchActorRequest
	err = core_http_request.DecodeAndValidateRequest(r, &req)
	if err != nil {
		responseHandler.ErrorResponse(err, "ошибка валидации запроса")

		return
	}

	actorPatch, err := actorPatchFromRequest(req)
	if err != nil {
		responseHandler.ErrorResponse(err, "некорректные данные запроса")

		return
	}

	actor, err := h.actorService.PatchActor(ctx, id, actorPatch)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось обновить актера/актрису")

		return
	}

	response := PatchActorResponse(actorDTOFromDomain(actor))

	responseHandler.JSONResponse(response, http.StatusOK)
}

func actorPatchFromRequest(request PatchActorRequest) (domain.ActorPatch, error) {
	var firstName *string
	if request.FirstName.Set {
		firstName = &request.FirstName.Value
	}

	var lastName *string
	if request.LastName.Set {
		lastName = &request.LastName.Value
	}

	var birthDate *time.Time
	if request.BirthDate.Set {
		t, err := time.Parse(domain.DateLayout, request.BirthDate.Value)
		if err != nil {
			return domain.ActorPatch{}, err
		}

		birthDate = &t
	}

	return domain.NewActorPatch(firstName, lastName, request.MiddleName, request.Description, birthDate), nil
}
