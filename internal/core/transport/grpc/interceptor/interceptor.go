package core_grpc_interceptor

import (
	"context"
	"time"

	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Кладёт логгер в контекст. Без него core_logger.FromContext в сервисном слое упадёт с паникой:
// в HTTP это делает middleware Logger, а здесь — мы
func LoggerInterceptor(logger *core_logger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		log := logger.With(zap.String("grpc_method", info.FullMethod))
		ctx = core_logger.ToContext(ctx, log)

		start := time.Now()
		resp, err := handler(ctx, req)

		log.Debug(
			"gRPC-запрос",
			zap.String("code", status.Code(err).String()),
			zap.Duration("duration", time.Since(start)),
		)

		return resp, err
	}
}

func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if p := recover(); p != nil {
				core_logger.FromContext(ctx).Error("паника в gRPC-обработчике", zap.Any("panic", p))
				err = status.Error(codes.Internal, "внутренняя ошибка")
			}
		}()

		return handler(ctx, req)
	}
}
