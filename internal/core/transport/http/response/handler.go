package core_http_response

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_request "github.com/vasya2314/golang-kp/internal/core/transport/http/request"
	"go.uber.org/zap"
)

type HTTPResponseHandler struct {
	log *core_logger.Logger
	rw  http.ResponseWriter
}

func NewHTTPResponseHandler(
	log *core_logger.Logger,
	rw http.ResponseWriter,
) *HTTPResponseHandler {
	return &HTTPResponseHandler{
		log: log,
		rw:  rw,
	}
}

func (h *HTTPResponseHandler) NoContentResponse() {
	h.rw.WriteHeader(http.StatusNoContent)
}

func (h *HTTPResponseHandler) StreamFileResponse(file domain.File) {
	h.rw.Header().Set("Content-Type", file.ContentType)
	h.rw.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	// Браузер не будет «угадывать» тип и не исполнит, например, подсунутый HTML
	h.rw.Header().Set("X-Content-Type-Options", "nosniff")
	h.rw.WriteHeader(http.StatusOK)

	// Байты идут из S3 сразу клиенту, кусками по 32 КБ, без загрузки в память целиком
	if _, err := io.Copy(h.rw, file.Content); err != nil {
		// Заголовки уже отправлены, ответить JSON-ошибкой нельзя — только залогировать
		h.log.Warn("обрыв при отдаче постера", zap.Error(err))
	}
}

func (h *HTTPResponseHandler) JSONResponse(
	responseBody any,
	statusCode int,
) {
	h.rw.WriteHeader(statusCode)

	if err := json.NewEncoder(h.rw).Encode(responseBody); err != nil {
		h.log.Error("ошибка записи HTTP-ответа", zap.Error(err))
	}
}

func (h *HTTPResponseHandler) ErrorResponse(err error, msg string) {
	var (
		statusCode int
		logFunc    func(string, ...zap.Field)
	)

	switch {
	case errors.Is(err, core_errors.ErrInvalidArgument):
		statusCode = http.StatusBadRequest
		logFunc = h.log.Warn

	case errors.Is(err, core_errors.ErrNotFound):
		statusCode = http.StatusNotFound
		logFunc = h.log.Debug

	case errors.Is(err, core_errors.ErrConflict):
		statusCode = http.StatusConflict
		logFunc = h.log.Warn

	case errors.Is(err, core_errors.ErrPayloadTooLarge):
		statusCode = http.StatusRequestEntityTooLarge
		logFunc = h.log.Warn

	case errors.Is(err, core_errors.ErrUnsupportedMediaType):
		statusCode = http.StatusUnsupportedMediaType
		logFunc = h.log.Warn

	case errors.Is(err, core_errors.ErrServiceUnavailable):
		statusCode = http.StatusServiceUnavailable
		logFunc = h.log.Warn

	default:
		statusCode = http.StatusInternalServerError
		logFunc = h.log.Error
	}

	logFunc(msg, zap.Error(err))

	h.errorResponse(statusCode, err, msg)
}

func (h *HTTPResponseHandler) PanicResponse(p any, msg string) {
	statusCode := http.StatusInternalServerError
	err := fmt.Errorf("непредвиденная паника: %v", p)

	h.log.Error(msg, zap.Error(err))

	h.errorResponse(statusCode, err, msg)
}

func (h *HTTPResponseHandler) errorResponse(
	statusCode int,
	err error,
	msg string,
) {
	var ve *core_http_request.ValidationError

	if errors.As(err, &ve) {
		h.JSONResponse(map[string]any{
			"message": msg,
			"errors":  ve.Fields,
		}, statusCode)

		return
	}

	response := map[string]string{
		"message": msg,
	}

	// Текст внутренних ошибок (SQL, паники) клиенту не отдаём — он уже записан в лог
	if statusCode < http.StatusInternalServerError {
		response["error"] = err.Error()
	}

	h.JSONResponse(response, statusCode)
}
