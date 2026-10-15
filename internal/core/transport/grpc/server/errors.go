package core_grpc_server

import (
	"errors"
	"fmt"

	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToStatus — на стороне сервера: наша ошибка → gRPC-статус
func ToStatus(err error) error {
	var code codes.Code

	switch {
	case errors.Is(err, core_errors.ErrNotFound):
		code = codes.NotFound

	case errors.Is(err, core_errors.ErrInvalidArgument):
		code = codes.InvalidArgument

	case errors.Is(err, core_errors.ErrConflict):
		code = codes.Aborted

	default:
		// Как и в HTTP: текст внутренних ошибок наружу не отдаём
		return status.Error(codes.Internal, "внутренняя ошибка")
	}

	return status.Error(code, err.Error())
}

// FromStatus — на стороне клиента: gRPC-статус → наша ошибка
func FromStatus(err error) error {
	st, _ := status.FromError(err)

	switch st.Code() {
	case codes.NotFound:
		return fmt.Errorf("%s: %w", st.Message(), core_errors.ErrNotFound)

	case codes.InvalidArgument:
		return fmt.Errorf("%s: %w", st.Message(), core_errors.ErrInvalidArgument)

	case codes.Unavailable, codes.DeadlineExceeded:
		return fmt.Errorf("%s: %w", st.Message(), core_errors.ErrServiceUnavailable)

	default:
		return fmt.Errorf("grpc: %w", err)
	}
}
