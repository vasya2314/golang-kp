package core_grpc_server

import (
	"context"
	"fmt"
	"net"
	"time"

	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_grpc_interceptor "github.com/vasya2314/golang-kp/internal/core/transport/grpc/interceptor"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	*grpc.Server
	config Config
	logger *core_logger.Logger
}

func NewServer(config Config, logger *core_logger.Logger) *Server {
	server := grpc.NewServer(
		// Первый в списке — внешний: логгер должен оказаться в ctx раньше, чем сработает recovery
		grpc.ChainUnaryInterceptor(
			core_grpc_interceptor.LoggerInterceptor(logger),
			core_grpc_interceptor.RecoveryInterceptor(),
		),
	)

	// Позволяет grpcurl узнать список методов без .proto-файлов. В проде часто выключают
	reflection.Register(server)

	return &Server{Server: server, config: config, logger: logger}
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.Address)
	if err != nil {
		return fmt.Errorf("открытие порта %s: %w", s.config.Address, err)
	}

	ch := make(chan error, 1)

	go func() {
		s.logger.Warn("Starting gRPC server", zap.String("address", s.config.Address))
		ch <- s.Serve(listener)
	}()

	select {
	case err = <-ch:
		return fmt.Errorf("работа gRPC-сервера: %w", err)
	case <-ctx.Done():
		s.logger.Warn("Shutting down gRPC server...")

		stopped := make(chan struct{})
		go func() {
			// Ждёт, пока завершатся текущие запросы, новых не принимает
			s.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
		case <-time.After(s.config.ShutdownTimeout):
			// Не дождались — рвём соединения
			s.Stop()
		}

		s.logger.Warn("gRPC server stopped")

		return nil
	}
}
