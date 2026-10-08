package movie_transport_http

import (
	"net/http"
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_request "github.com/vasya2314/golang-kp/internal/core/transport/http/request"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

type CreateMovieRequest struct {
	Title       string  `json:"title" validate:"required,min=3,max=100"`
	Description *string `json:"description" validate:"omitempty,max=1000"`
	ReleaseAt   string  `json:"release_at" validate:"required,datetime=2006-01-02"`
}

type CreateMovieResponse MovieDTOResponse

func (h *MovieHTTPHandler) CreateMovie(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	var request CreateMovieRequest
	err := core_http_request.DecodeAndValidateRequest(r, &request)
	if err != nil {
		responseHandler.ErrorResponse(err, "ошибка валидации запроса")

		return
	}

	year, err := time.Parse(domain.DateLayout, request.ReleaseAt)
	if err != nil {
		responseHandler.ErrorResponse(err, "дата указана в неверном формате")

		return
	}

	movieDomain := domain.NewMovieUninitialized(request.Title, request.Description, year)

	movieDomain, err = h.movieService.CreateMovie(ctx, movieDomain)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось создать фильм")

		return
	}

	response := CreateMovieResponse(movieDTOFromDomain(movieDomain))

	responseHandler.JSONResponse(response, http.StatusCreated)
}
