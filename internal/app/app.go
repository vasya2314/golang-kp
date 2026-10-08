package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"
	core_http_middleware "github.com/vasya2314/golang-kp/internal/core/transport/http/middleware"
	core_http_server "github.com/vasya2314/golang-kp/internal/core/transport/http/server"
	actor_postgres_repository "github.com/vasya2314/golang-kp/internal/modules/actors/repository/postgres"
	actor_service "github.com/vasya2314/golang-kp/internal/modules/actors/service"
	actor_transport_http "github.com/vasya2314/golang-kp/internal/modules/actors/transport"
	movie_postgres_repository "github.com/vasya2314/golang-kp/internal/modules/movies/repository/postgres"
	movie_service "github.com/vasya2314/golang-kp/internal/modules/movies/service"
	movie_transport_http "github.com/vasya2314/golang-kp/internal/modules/movies/transport/http"
	"go.uber.org/zap"
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

	logger.Debug("Initializing movie module")

	movieRepository := movie_postgres_repository.NewMovieRepository(*pool)
	movieService := movie_service.NewMovieService(movieRepository)
	movieTransportHTTP := movie_transport_http.NewMovieHTTPHandler(movieService)

	logger.Debug("Initializing actor module")

	actorRepository := actor_postgres_repository.NewActorRepository(*pool)
	actorService := actor_service.NewActorService(actorRepository)
	actorTransportHTTP := actor_transport_http.NewActorHTTPHandler(actorService)

	logger.Debug("Initializing router")

	apiVersionRouterV1 := core_http_server.NewAPIVersionRouter(core_http_server.ApiVersion1)

	movieTransportHTTP.Routes(apiVersionRouterV1.Mux)
	actorTransportHTTP.Routes(apiVersionRouterV1.Mux)

	apiVersionRouterV1.RegisterRoutes(server.Mux)

	logger.Debug("Starting server...")

	err = server.Run(ctx)
	if err != nil {
		err = fmt.Errorf("не удалось запустить сервер: %w", err)
		panic(err)
	}
}
