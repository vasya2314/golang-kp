package movie_transport_http

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

type PatchMovieRequest struct {
	Title       domain.Optional[string] `json:"title"       validate:"omitempty,notnull,min=3,max=100"`
	Description domain.Optional[string] `json:"description" validate:"omitempty,null|max=1000"`
	ReleaseAt   domain.Optional[string] `json:"release_at"  validate:"omitempty,notnull,datetime=2006-01-02"`
}

type PatchMovieResponse MovieDTOResponse

func (h *MovieHTTPHandler) PatchMovie(w http.ResponseWriter, r *http.Request) {
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

	var req PatchMovieRequest
	err = core_http_request.DecodeAndValidateRequest(r, &req)
	if err != nil {
		responseHandler.ErrorResponse(err, "ошибка валидации запроса")

		return
	}

	moviePatch, err := moviePatchFromRequest(req)
	if err != nil {
		responseHandler.ErrorResponse(err, "некорректные данные запроса")

		return
	}

	movie, err := h.movieService.PatchMovie(ctx, id, moviePatch)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось обновить фильм")

		return
	}

	response := PatchMovieResponse(movieDTOFromDomain(movie))

	responseHandler.JSONResponse(response, http.StatusOK)
}

func moviePatchFromRequest(request PatchMovieRequest) (domain.MoviePatch, error) {
	var title *string
	if request.Title.Set {
		title = &request.Title.Value
	}

	var releaseAt *time.Time
	if request.ReleaseAt.Set {
		t, err := time.Parse(domain.DateLayout, request.ReleaseAt.Value)
		if err != nil {
			return domain.MoviePatch{}, err
		}

		releaseAt = &t
	}

	return domain.NewMoviePatch(title, request.Description, releaseAt), nil
}
