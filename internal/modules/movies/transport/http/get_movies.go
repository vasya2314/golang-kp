package movie_transport_http

import (
	"fmt"
	"net/http"
	"strconv"

	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

type GetMoviesResponse []MovieDTOResponse

func (h *MovieHTTPHandler) GetMovies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	var offset *int

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		o, err := strconv.Atoi(offsetStr)
		if err != nil {
			err = fmt.Errorf("получение `offset`: %w", core_errors.ErrInvalidArgument)
			responseHandler.ErrorResponse(err, "неверное значение параметра `offset`")

			return
		}

		offset = &o
	}

	var limit *int

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil {
			err = fmt.Errorf("получение `limit`: %w", core_errors.ErrInvalidArgument)
			responseHandler.ErrorResponse(err, "неверное значение параметра `limit`")

			return
		}

		limit = &l
	}

	movies, err := h.movieService.GetMovies(ctx, limit, offset)
	if err != nil {
		responseHandler.ErrorResponse(err, "ошибка получения списка фильмов")

		return
	}

	response := GetMoviesResponse(movieDTOFromDomains(movies))

	responseHandler.JSONResponse(response, http.StatusOK)
}
