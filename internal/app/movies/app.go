package app_movies

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/twmb/franz-go/pkg/kgo"
	core_kafka "github.com/vasya2314/golang-kp/internal/core/kafka"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"
	core_s3_storage "github.com/vasya2314/golang-kp/internal/core/storage/s3"
	core_http_middleware "github.com/vasya2314/golang-kp/internal/core/transport/http/middleware"
	core_http_server "github.com/vasya2314/golang-kp/internal/core/transport/http/server"
	movie_actors_client "github.com/vasya2314/golang-kp/internal/modules/movies/client/actors"
	movie_postgres_repository "github.com/vasya2314/golang-kp/internal/modules/movies/repository/postgres"
	movie_service "github.com/vasya2314/golang-kp/internal/modules/movies/service"
	movie_transport_http "github.com/vasya2314/golang-kp/internal/modules/movies/transport/http"
	movie_transport_kafka "github.com/vasya2314/golang-kp/internal/modules/movies/transport/kafka"
	actors_events "github.com/vasya2314/golang-kp/pkg/events/actors"
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

	logger.Debug("Initializing s3 storage")

	fileStorage, err := core_s3_storage.NewStorage(ctx, core_s3_storage.NewConfigMust())
	if err != nil {
		logger.Fatal("не удалось инициализировать S3-хранилище", zap.Error(err))
	}

	logger.Debug("Initializing actors gRPC client")

	actorsClient, err := movie_actors_client.NewActorsClient(movie_actors_client.MustLoadConfig())
	if err != nil {
		logger.Fatal("не удалось создать gRPC-клиент actors", zap.Error(err))
	}
	defer actorsClient.Close()

	logger.Debug("Initializing movie module")

	movieRepository := movie_postgres_repository.NewMovieRepository(*pool)
	movieService := movie_service.NewMovieService(movieRepository, fileStorage, actorsClient)
	movieTransportHTTP := movie_transport_http.NewMovieHTTPHandler(movieService)

	logger.Debug("Initializing router")

	apiVersionRouterV1 := core_http_server.NewAPIVersionRouter(core_http_server.ApiVersion1)

	movieTransportHTTP.Routes(apiVersionRouterV1.Mux)

	apiVersionRouterV1.RegisterRoutes(server.Mux)

	logger.Debug("Initializing kafka consumer")

	kafkaClient, err := core_kafka.NewClient(
		ctx,
		core_kafka.NewConfigMust(),
		kgo.ConsumerGroup(movie_transport_kafka.ConsumerGroup), // имя «команды читателей»
		kgo.ConsumeTopics(actors_events.Topic),                 // что читать
		// Коммитим offset сами, после обработки. Автокоммит срабатывает по таймеру и может
		// отметить ещё не обработанное сообщение — при падении оно потеряется
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		logger.Fatal("не удалось подключиться к kafka", zap.Error(err))
	}
	defer kafkaClient.Close()

	consumer := movie_transport_kafka.NewActorEventsConsumer(kafkaClient, movieService, logger)

	logger.Debug("Starting server and consumer...")

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		return server.Run(groupCtx)
	})
	group.Go(func() error {
		return consumer.Run(groupCtx)
	})

	if err = group.Wait(); err != nil {
		logger.Fatal("сервис остановлен с ошибкой", zap.Error(err))
	}
}
