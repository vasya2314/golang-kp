package app_actors

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	core_kafka "github.com/vasya2314/golang-kp/internal/core/kafka"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_outbox "github.com/vasya2314/golang-kp/internal/core/outbox"
	core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"
	core_grpc_server "github.com/vasya2314/golang-kp/internal/core/transport/grpc/server"
	core_http_middleware "github.com/vasya2314/golang-kp/internal/core/transport/http/middleware"
	core_http_server "github.com/vasya2314/golang-kp/internal/core/transport/http/server"
	actor_postgres_repository "github.com/vasya2314/golang-kp/internal/modules/actors/repository/postgres"
	actor_service "github.com/vasya2314/golang-kp/internal/modules/actors/service"
	actor_transport_grpc "github.com/vasya2314/golang-kp/internal/modules/actors/transport/grpc"
	actor_transport_http "github.com/vasya2314/golang-kp/internal/modules/actors/transport/http"
	actorsv1 "github.com/vasya2314/golang-kp/pkg/api/actors/v1"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func Run() {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	logger, err := core_logger.NewLogger(core_logger.NewConfigMust())
	if err != nil {
		fmt.Println("failed to initialize logger", err)
		os.Exit(1)
	}
	defer logger.Close()

	logger.Debug("initializing postgres connection pool")

	pool, err := core_pgx_pool.NewPool(ctx, core_pgx_pool.NewConfigMust())
	if err != nil {
		logger.Fatal("не удалось инициализировать пул подключений postgres", zap.Error(err))
	}
	defer pool.Close()

	server := core_http_server.NewHTTPServer(
		core_http_server.MustLoadConfig(),
		logger,
	)

	logger.Debug("Initializing middlewares")

	server.Use(
		core_http_middleware.RequestID(),
		core_http_middleware.Logger(logger),
		core_http_middleware.Trace(),
		core_http_middleware.Panic(),
	)

	logger.Debug("Initializing kafka client")

	kafkaClient, err := core_kafka.NewClient(ctx, core_kafka.NewConfigMust())
	if err != nil {
		logger.Fatal("не удалось подключиться к kafka", zap.Error(err))
	}
	defer kafkaClient.Close()

	relay := core_outbox.NewRelay(pool, kafkaClient, logger)

	logger.Debug("Initializing actor module")

	actorRepository := actor_postgres_repository.NewActorRepository(*pool)
	actorService := actor_service.NewActorService(actorRepository)
	actorTransportHTTP := actor_transport_http.NewActorHTTPHandler(actorService)

	logger.Debug("Initializing router")

	apiVersionRouterV1 := core_http_server.NewAPIVersionRouter(core_http_server.ApiVersion1)

	actorTransportHTTP.Routes(apiVersionRouterV1.Mux)

	apiVersionRouterV1.RegisterRoutes(server.Mux)

	logger.Debug("Initializing gRPC server")

	grpcServer := core_grpc_server.NewServer(core_grpc_server.MustLoadConfig(), logger)

	// по сути регистрация routes как в HTTP
	actorsv1.RegisterActorServiceServer(grpcServer, actor_transport_grpc.NewActorGRPCServer(actorService))

	logger.Debug("Starting servers...")

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		return server.Run(groupCtx)
	})
	group.Go(func() error {
		return grpcServer.Run(groupCtx)
	})
	group.Go(func() error {
		return relay.Run(groupCtx)
	})

	if err = group.Wait(); err != nil {
		logger.Fatal("сервис остановлен с ошибкой", zap.Error(err))
	}
}
