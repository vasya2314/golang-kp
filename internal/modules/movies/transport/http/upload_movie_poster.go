package movie_transport_http

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

const (
	maxPosterSize = 5 << 20 // 5 МБ
	// Запас на boundary и заголовки частей multipart
	multipartOverhead = 64 << 10
	// Сколько держать в памяти; всё, что больше, net/http сбросит во временный файл
	multipartMemory = 1 << 20
)

type UploadMoviePosterResponse MovieDTOResponse

func (h *MovieHTTPHandler) UploadMoviePoster(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		err = fmt.Errorf("получение `id`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр id")

		return
	}

	// Без этого клиент может прислать 10 ГБ, и мы честно начнём их читать
	r.Body = http.MaxBytesReader(w, r.Body, maxPosterSize+multipartOverhead)

	if err = r.ParseMultipartForm(multipartMemory); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			err = fmt.Errorf("размер запроса: %w", core_errors.ErrPayloadTooLarge)
			responseHandler.ErrorResponse(err, "файл больше 5 МБ")

			return
		}

		err = fmt.Errorf("разбор multipart: %v: %w", err, core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "ожидается multipart/form-data")

		return
	}

	// Удаляем временные файлы, которые ParseMultipartForm мог создать на диске
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("poster")
	if err != nil {
		err = fmt.Errorf("получение файла: %v: %w", err, core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "поле poster обязательно")

		return
	}
	defer file.Close()

	if header.Size == 0 || header.Size > maxPosterSize {
		err = fmt.Errorf("размер файла %d: %w", header.Size, core_errors.ErrPayloadTooLarge)
		responseHandler.ErrorResponse(err, "файл пустой или больше 5 МБ")

		return
	}

	contentType, err := detectContentType(file)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось прочитать файл")

		return
	}

	poster := domain.NewFile(file, header.Size, contentType)

	movie, err := h.movieService.UploadMoviePoster(ctx, id, poster)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось загрузить постер")

		return
	}

	response := UploadMoviePosterResponse(movieDTOFromDomain(movie))

	responseHandler.JSONResponse(response, http.StatusOK)
}

// Определяет тип по первым байтам содержимого и перематывает файл обратно в начало
func detectContentType(file multipart.File) (string, error) {
	head := make([]byte, 512)

	n, err := io.ReadFull(file, head)

	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("чтение заголовка файла: %w", err)
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("перемотка файла: %w", err)
	}

	return http.DetectContentType(head[:n]), nil
}
