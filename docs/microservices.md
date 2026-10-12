# Разделение на сервисы: movies-service и actors-service (gRPC + Kafka)

Разбор того, как из одного приложения сделать два сервиса со своими базами данных,
которые общаются синхронно через **gRPC** и асинхронно через **Kafka**.

Как и в `posters.md`, полный код здесь только для нового. Рутинные правки описаны словами,
а в конце разделов есть вопросы для самопроверки.

> **Честное предупреждение.** Для проекта такого размера в реальной жизни правильный ответ — модульный монолит,
> который у тебя уже есть. Микросервисы решают организационные проблемы (много команд, разные темпы
> релизов, разная нагрузка), а технических проблем добавляют. Здесь мы делим ради учёбы,
> и это нормально. Но на собеседовании и на работе важно уметь объяснить, **когда не надо** дробить.

## 1. Где модули связаны

Прежде чем резать, нужно найти все места, где `movies` лезет в данные `actors`.
Сейчас всё это делает Postgres, «бесплатно»:

| # | Где | Что делает БД | После разделения на две БД |
|---|---|---|---|
| 1 | `movie_actors.actor_id REFERENCES actors` | не даёт привязать несуществующего актёра | внешний ключ на таблицу в другой БД невозможен |
| 2 | `JOIN actors` в `GetMovieActors` | достаёт данные актёров одним запросом | JOIN между БД невозможен |
| 3 | `ON DELETE CASCADE` на `actor_id` | удаляет связи при удалении актёра | каскад между БД невозможен |

Каждую связь заменяем своим механизмом:

| # | Замена | Почему так |
|---|---|---|
| 1 | **gRPC** `GetActor` перед привязкой | нужен ответ **сейчас**, чтобы вернуть клиенту 404 |
| 2 | **gRPC** `BatchGetActors` по списку id | нужен ответ сейчас, и **одним** запросом, а не N |
| 3 | **Kafka**, событие `actor.deleted` | actors-service не должен знать, кому интересно удаление. Достаточно, чтобы связи удалились **рано или поздно** |

Отсюда главное правило выбора:

- **gRPC (синхронно):** «мне нужен ответ, чтобы ответить пользователю». Оба сервиса должны работать одновременно.
- **Kafka (асинхронно):** «у меня что-то произошло, кому надо — среагируйте». Отправитель не знает получателей,
  получатель может быть выключен и обработает событие потом.

## 2. Целевая картина

```
                    :5055 HTTP                          :5056 HTTP
клиент ──────────► movies-service                      actors-service ◄────────── клиент
                    │  │      ▲                         │   ▲      │
                    │  │      │  gRPC :9090              │   │      │ INSERT outbox
                    │  │      └── GetActor ─────────────┘   │      │ (в одной транзакции с DELETE)
                    │  │          BatchGetActors            │      ▼
                    │  │                                    │   outbox relay
                    │  │                                    │      │
                    │  │           Kafka: actors.events     │      │ actor.deleted
                    │  └──────── consumer ◄─────────────────┼──────┘
                    ▼                                       ▼
              Postgres: golang_kp                  Postgres: golang_kp_actors
              movies, movie_actors                 actors, outbox
              (+ S3 для постеров)
```

### Структура репозитория

Делаем **монорепозиторий**: один `go.mod`, две точки входа. Общий код (`internal/core`)
не копируется, а контракты между сервисами лежат рядом.

```
api/proto/actors/v1/actors.proto      ← новый: контракт gRPC
pkg/api/actors/v1/*.pb.go             ← сгенерированный код (коммитим)
pkg/events/actors/events.go           ← новый: контракт событий Kafka
buf.yaml, buf.gen.yaml                ← новые
cmd/
  movies-service/main.go              ← новый (вместо cmd/golang-kp)
  actors-service/main.go              ← новый
internal/app/
  movies/app.go                       ← бывший app.go, только модуль movies
  actors/app.go                       ← новый, только модуль actors
internal/core/
  transport/grpc/                     ← новый: сервер, interceptor-ы, перевод ошибок
  kafka/                              ← новый: конфиг и клиент
  outbox/                             ← новый: relay
  domain/outbox.go                    ← новый
internal/modules/actors/
  transport/grpc/server.go            ← новый
internal/modules/movies/
  client/actors/client.go             ← новый: gRPC-клиент к actors
  transport/kafka/consumer.go         ← новый
migrations/
  movies/                             ← бывшая migrations/ (git mv)
  actors/                             ← новый
```

**Почему `pkg/`, а не `internal/`.** Сгенерированный gRPC-код и события — **публичный контракт**.
Если завтра actors-service уедет в отдельный репозиторий, movies-service всё равно должен их импортировать.
А `internal/` по правилам Go из чужого модуля не импортируется.

## 3. План: делим поэтапно, а не всё сразу

Это главный урок раздела. В реальных проектах монолит **никогда** не режут одним большим PR.
Используют паттерн **Strangler Fig**: маленькие шаги, и после каждого система работает.

| Шаг | Что делаем | База данных | Проверка |
|---|---|---|---|
| 1 | Две точки входа, каждая со своим модулем | всё ещё **общая** | все ручки работают на двух портах |
| 2 | gRPC вместо FK-проверки и JOIN | общая | movies не читает таблицу `actors` |
| 3 | Kafka + outbox вместо `ON DELETE CASCADE` | общая, FK на `actors` удалён | удаление актёра чистит связи через Kafka |
| 4 | Переносим `actors` в свою БД | **раздельные** | movies-service работает без таблицы `actors` |

Сначала **разрываем связи в коде** (шаги 1–3), и только потом **разносим данные** (шаг 4).
Если сделать наоборот, на время переделки всё будет сломано.

Делай коммит после каждого шага.

## 4. Окружение

### Kafka в docker-compose

Kafka 4.x работает без ZooKeeper, в режиме KRaft: один контейнер — это и брокер, и контроллер.

```yaml
  golang-kp-kafka:
    image: apache/kafka:4.0.0
    container_name: golang-kp-kafka
    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: broker,controller
      CLUSTER_ID: 4L6g3nShT-eMCtK--X86sw
      # INTERNAL — для контейнеров внутри compose, EXTERNAL — для сервисов, запущенных на хосте
      KAFKA_LISTENERS: INTERNAL://:29092,EXTERNAL://:9092,CONTROLLER://:9093
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://golang-kp-kafka:29092,EXTERNAL://localhost:9092
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT
      KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@golang-kp-kafka:9093
      # Один брокер — служебные топики не могут иметь больше одной реплики
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: 1
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: 0
      # Топики создаём явно, как в проде, а не «как получится» при первой записи
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"
    volumes:
      - ${PROJECT_ROOT}/out/kafkadata:/var/lib/kafka/data
    ports:
      - "127.0.0.1:9092:9092"
    restart: always
```

**Самое частое место, где всё ломается — advertised listeners.** Клиент подключается к `localhost:9092`,
а брокер отвечает: «за данными ходи по адресу X». Если X — имя контейнера, сервис на хосте его не найдёт.
Поэтому слушателей два: для хоста и для соседей по compose.

`Makefile`:

```make
kafka-create-topics:
	@docker compose exec -T golang-kp-kafka /opt/kafka/bin/kafka-topics.sh \
		--bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic actors.events \
		--partitions 3 \
		--replication-factor 1

kafka-tail:
	@docker compose exec golang-kp-kafka /opt/kafka/bin/kafka-console-consumer.sh \
		--bootstrap-server localhost:9092 \
		--topic $(topic) \
		--from-beginning \
		--property print.key=true
```

`make kafka-tail topic=actors.events` покажет все сообщения в топике. Это основной инструмент отладки.

### Инструменты

```bash
brew install bufbuild/buf/buf grpcurl

go get google.golang.org/grpc \
       google.golang.org/protobuf \
       github.com/twmb/franz-go/pkg/kgo \
       golang.org/x/sync/errgroup
```

- **buf** генерирует код из `.proto`, проверяет стиль (`buf lint`) и ловит ломающие изменения контракта (`buf breaking`).
  В индустрии он заменил ручные вызовы `protoc`.
- **grpcurl** — как `curl`, только для gRPC.
- **franz-go** — Kafka-клиент на чистом Go, быстрый и с полной поддержкой протокола. Альтернативы:
  `segmentio/kafka-go` (проще, но беднее) и `confluent-kafka-go` (обёртка над C-библиотекой `librdkafka`, требует cgo).

## 5. Шаг 1 — две точки входа

Цель: два процесса, каждый поднимает **только свой** модуль. База пока общая.

1. `git mv internal/app/app.go internal/app/movies/app.go`, пакет `app_movies`. Убери оттуда всё про актёров.
2. Создай `internal/app/actors/app.go` по образцу: только модуль actors.
3. `cmd/movies-service/main.go` и `cmd/actors-service/main.go` вызывают свой `Run()`. `cmd/golang-kp` удали.
4. `Makefile`: вместо `app-run` два таргета, у каждого свои порт и папка логов:

```make
run-movies:
	@export LOGGER_FOLDER=${PROJECT_ROOT}/out/logs/movies && \
	export POSTGRES_HOST=localhost && \
	export HTTP_ADDRESS=:5055 && \
	go run ${PROJECT_ROOT}/cmd/movies-service

run-actors:
	@export LOGGER_FOLDER=${PROJECT_ROOT}/out/logs/actors && \
	export POSTGRES_HOST=localhost && \
	export HTTP_ADDRESS=:5056 && \
	go run ${PROJECT_ROOT}/cmd/actors-service
```

Запусти оба сервиса в двух терминалах. `/api/v1/movies/...` работает на `:5055`, `/api/v1/actors/...` — на `:5056`.

**Проверка шага:** `GET :5055/api/v1/movies/1/actors` всё ещё работает. Почему? (Ответ: JOIN по-прежнему
читает таблицу `actors` из общей БД. Именно эту невидимую связь разрываем на шаге 2.)

## 6. Шаг 2 — gRPC

### 6.1. Контракт

`api/proto/actors/v1/actors.proto`

```proto
syntax = "proto3";

package actors.v1;

// Внутренний API сервиса актёров. Наружу (клиентам) не публикуется.
service ActorService {
  rpc GetActor(GetActorRequest) returns (GetActorResponse);
  // Один запрос на список id, чтобы не получить N+1 по сети
  rpc BatchGetActors(BatchGetActorsRequest) returns (BatchGetActorsResponse);
}

message Actor {
  int64 id = 1;
  int64 version = 2;
  string first_name = 3;
  string last_name = 4;
  optional string middle_name = 5;
  optional string description = 6;
  // Формат YYYY-MM-DD
  string birth_date = 7;
}

message GetActorRequest {
  int64 id = 1;
}

message GetActorResponse {
  Actor actor = 1;
}

message BatchGetActorsRequest {
  repeated int64 ids = 1;
}

message BatchGetActorsResponse {
  // Только найденные. Отсутствующие id — не ошибка, а просто нет в ответе
  repeated Actor actors = 1;
}
```

Что важно в `.proto`:

- **Номера полей (`= 1`, `= 2`) — это и есть контракт**, а не имена. По проводу идут номера.
  Поле можно переименовать, но **нельзя** менять номер или тип и **нельзя** повторно использовать номер удалённого поля
  (для этого есть `reserved 5;`). Нарушение молча сломает совместимость со старыми клиентами.
- **`optional`** в proto3 даёт в Go `*string`, то есть ровно то, что у нас в домене для `MiddleName`.
- **`v1` в пути и в package.** Несовместимые изменения делают в `v2`, а старая версия живёт рядом, пока
  все клиенты не переедут.
- **Request/Response на каждый метод**, даже если внутри одно поле. Иначе потом не добавишь параметр без поломки.
  Это проверяет `buf lint`.

### 6.2. Генерация

`buf.yaml`:

```yaml
version: v2
modules:
  - path: api/proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

`buf.gen.yaml`:

```yaml
version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/vasya2314/golang-kp/pkg/api
plugins:
  - remote: buf.build/protocolbuffers/go
    out: pkg/api
    opt: paths=source_relative
  - remote: buf.build/grpc/go
    out: pkg/api
    opt: paths=source_relative
```

`Makefile`:

```make
proto-gen:
	@buf lint && buf generate
```

После `make proto-gen` появятся `pkg/api/actors/v1/actors.pb.go` (сообщения) и `actors_grpc.pb.go` (клиент и сервер),
пакет `actorsv1`. Посмотри, что сгенерировалось: интерфейс `ActorServiceServer`, структура
`UnimplementedActorServiceServer`, функции `RegisterActorServiceServer` и `NewActorServiceClient`.

**Сгенерированный код коммитим.** Так `go build` работает без `buf`, а в diff PR видно, как меняется API.

### 6.3. Core: ошибки gRPC

У gRPC свои коды ошибок вместо HTTP-статусов. Нужен перевод в обе стороны, как в `ErrorResponse` для HTTP.

`internal/core/transport/grpc/errors.go`

```go
package core_grpc

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
```

И в `ErrorResponse` (HTTP) добавь `ErrServiceUnavailable → 503`. Теперь, если actors-service лежит,
клиент movies-service получит честный 503, а не 500.

### 6.4. Core: сервер и interceptor-ы

**Interceptor** — это middleware для gRPC. Нам нужны два, по аналогии с HTTP-middleware.

`internal/core/transport/grpc/interceptors.go`

```go
package core_grpc

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
```

`internal/core/transport/grpc/server.go`: конфиг (префикс `GRPC`, поля `ADDRESS` и `SHUTDOWN_TIMEOUT`)
напиши сам по образцу HTTP. Сервер:

```go
type Server struct {
	*grpc.Server
	config Config
	logger *core_logger.Logger
}

func NewServer(config Config, logger *core_logger.Logger) *Server {
	server := grpc.NewServer(
		// Первый в списке — внешний: логгер должен оказаться в ctx раньше, чем сработает recovery
		grpc.ChainUnaryInterceptor(
			LoggerInterceptor(logger),
			RecoveryInterceptor(),
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
```

Сравни с `core_http_server.Run`: та же схема graceful shutdown, только у gRPC `GracefulStop` не принимает контекст,
поэтому таймаут делается вручную.

### 6.5. actors-service: gRPC-сервер

**Репозиторий.** Добавь `GetActorsByIDs(ctx, ids []int) ([]domain.Actor, error)`:

```sql
SELECT id, version, first_name, last_name, middle_name, description, birth_date
FROM actors
WHERE id = ANY($1)
ORDER BY last_name, first_name;
```

pgx сам превращает `[]int` в массив Postgres. Сервис добавь сам: это простой проброс.

`internal/modules/actors/transport/grpc/server.go`

```go
package actor_transport_grpc

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_grpc "github.com/vasya2314/golang-kp/internal/core/transport/grpc"
	actorsv1 "github.com/vasya2314/golang-kp/pkg/api/actors/v1"
)

const maxBatchSize = 100

type ActorService interface {
	GetActor(ctx context.Context, id int) (domain.Actor, error)
	GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error)
}

type ActorGRPCServer struct {
	// Если в .proto добавят метод, а мы его не реализуем, код всё равно скомпилируется,
	// а метод вернёт codes.Unimplemented
	actorsv1.UnimplementedActorServiceServer

	actorService ActorService
}

func NewActorGRPCServer(actorService ActorService) *ActorGRPCServer {
	return &ActorGRPCServer{actorService: actorService}
}

func (s *ActorGRPCServer) GetActor(
	ctx context.Context,
	req *actorsv1.GetActorRequest,
) (*actorsv1.GetActorResponse, error) {
	actor, err := s.actorService.GetActor(ctx, int(req.GetId()))
	if err != nil {
		return nil, core_grpc.ToStatus(err)
	}

	return &actorsv1.GetActorResponse{Actor: actorToProto(actor)}, nil
}

func (s *ActorGRPCServer) BatchGetActors(
	ctx context.Context,
	req *actorsv1.BatchGetActorsRequest,
) (*actorsv1.BatchGetActorsResponse, error) {
	if len(req.GetIds()) > maxBatchSize {
		err := fmt.Errorf("не больше %d id за запрос: %w", maxBatchSize, core_errors.ErrInvalidArgument)

		return nil, core_grpc.ToStatus(err)
	}

	ids := make([]int, len(req.GetIds()))
	for i, id := range req.GetIds() {
		ids[i] = int(id)
	}

	actors, err := s.actorService.GetActorsByIDs(ctx, ids)
	if err != nil {
		return nil, core_grpc.ToStatus(err)
	}

	response := &actorsv1.BatchGetActorsResponse{
		Actors: make([]*actorsv1.Actor, len(actors)),
	}
	for i, actor := range actors {
		response.Actors[i] = actorToProto(actor)
	}

	return response, nil
}

func actorToProto(actor domain.Actor) *actorsv1.Actor {
	return &actorsv1.Actor{
		Id:          int64(actor.ID),
		Version:     int64(actor.Version),
		FirstName:   actor.FirstName,
		LastName:    actor.LastName,
		MiddleName:  actor.MiddleName,
		Description: actor.Description,
		BirthDate:   actor.BirthDate.Format(domain.DateLayout),
	}
}
```

Обрати внимание: это ещё один **transport**, рядом с HTTP. Сервис и репозиторий не изменились, кроме нового метода.
Именно ради этого слои и разделяли: тот же `ActorService` теперь обслуживает и HTTP, и gRPC.

**Почему `req.GetId()`, а не `req.Id`.** Геттеры сгенерированного кода не паникуют на `nil`-сообщении.

**Сборка в `app_actors`.** HTTP- и gRPC-сервер должны работать **одновременно** в одном процессе.
Для этого есть `errgroup`:

```go
grpcServer := core_grpc.NewServer(core_grpc.MustLoadConfig(), logger)
actorsv1.RegisterActorServiceServer(grpcServer, actor_transport_grpc.NewActorGRPCServer(actorService))

group, groupCtx := errgroup.WithContext(ctx)
group.Go(func() error { return httpServer.Run(groupCtx) })
group.Go(func() error { return grpcServer.Run(groupCtx) })

if err := group.Wait(); err != nil {
	logger.Fatal("сервис остановлен с ошибкой", zap.Error(err))
}
```

Если один из серверов упадёт, `groupCtx` отменится, второй корректно остановится, и процесс завершится.
Это принцип **fail fast**: лучше умереть целиком и дать оркестратору (docker/k8s) перезапустить процесс,
чем жить «наполовину».

В `run-actors` добавь `export GRPC_ADDRESS=:9090`.

**Проверка:**

```bash
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext -d '{"id": 1}'          localhost:9090 actors.v1.ActorService/GetActor
grpcurl -plaintext -d '{"id": 999}'        localhost:9090 actors.v1.ActorService/GetActor       # NotFound
grpcurl -plaintext -d '{"ids": [1,2,999]}' localhost:9090 actors.v1.ActorService/BatchGetActors
```

### 6.6. movies-service: gRPC-клиент

`internal/modules/movies/client/actors/client.go`. Конфиг с префиксом `ACTORS_GRPC` (поле `ADDRESS`) напиши сам.

```go
package movie_actors_client

import (
	"context"
	"fmt"
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_grpc "github.com/vasya2314/golang-kp/internal/core/transport/grpc"
	actorsv1 "github.com/vasya2314/golang-kp/pkg/api/actors/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Таймаут и ретраи описываются декларативно, и gRPC применяет их сам.
// Ретраим только UNAVAILABLE (сервис не ответил вообще) — оба метода только читают, повтор безопасен
const serviceConfig = `{
  "methodConfig": [{
    "name": [{"service": "actors.v1.ActorService"}],
    "timeout": "2s",
    "retryPolicy": {
      "maxAttempts": 3,
      "initialBackoff": "0.1s",
      "maxBackoff": "1s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

type ActorsClient struct {
	conn   *grpc.ClientConn
	client actorsv1.ActorServiceClient
}

func NewActorsClient(config Config) (*ActorsClient, error) {
	// NewClient не подключается сразу: соединение установится при первом вызове
	// и будет переиспользоваться всеми запросами (HTTP/2, мультиплексирование)
	conn, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("создание gRPC-клиента actors: %w", err)
	}

	return &ActorsClient{
		conn:   conn,
		client: actorsv1.NewActorServiceClient(conn),
	}, nil
}

func (c *ActorsClient) Close() error {
	return c.conn.Close()
}

func (c *ActorsClient) GetActor(ctx context.Context, id int) (domain.Actor, error) {
	resp, err := c.client.GetActor(ctx, &actorsv1.GetActorRequest{Id: int64(id)})
	if err != nil {
		return domain.Actor{}, fmt.Errorf("actors.GetActor: %w", core_grpc.FromStatus(err))
	}

	return actorFromProto(resp.GetActor())
}
```

`GetActorsByIDs` и `actorFromProto` (обратный перевод, включая `time.Parse(domain.DateLayout, ...)`) напиши сам.

**Самопроверка:** почему мы не ретраим `DEADLINE_EXCEEDED` и `INTERNAL`? Что было бы, если бы метод не читал,
а, например, списывал деньги?

### 6.7. movies-service: сервис и репозиторий

В `service.go` объяви интерфейс провайдера актёров. Это тот же приём, что с `FileStorage`:
сервис не знает, что за ним gRPC.

```go
type ActorProvider interface {
	GetActor(ctx context.Context, id int) (domain.Actor, error)
	GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error)
}
```

**`AddMovieActor`.** Сначала `s.actorProvider.GetActor(ctx, actorID)` (нет актёра — 404, actors-service лежит — 503),
потом `repo.AddMovieActor`. В репозитории ошибка FK `23503` теперь может означать только «нет фильма»: поправь текст ошибки.

**`GetMovieActors`.** JOIN заменяется двумя шагами:

1. Репозиторий: `GetMovieActorIDs(ctx, movieID) ([]int, error)` → `SELECT actor_id FROM movie_actors WHERE movie_id = $1`.
2. Сервис: если список пустой, сразу вернуть `[]` (не ходить по сети зря), иначе `s.actorProvider.GetActorsByIDs(ctx, ids)`.

Старый метод репозитория `GetMovieActors` и `movieActorModel` удали. **С этого момента movies-service не читает таблицу `actors`.**

**N+1 по сети.** Если бы мы звали `GetActor` в цикле по 30 актёрам фильма, вышло бы 30 сетевых запросов.
По 2 мс каждый — уже 60 мс вместо 2. Поэтому batch-метод — обязательная часть контракта, а не оптимизация «на потом».

**Самопроверка:** `BatchGetActors` вернул 2 актёра из 3 id. Когда так бывает, и почему это не ошибка? (Ответ в разделе 7.6.)

## 7. Шаг 3 — Kafka и outbox

### 7.1. Контракт события

`pkg/events/actors/events.go`

```go
package actorsevents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	Topic = "actors.events"

	TypeActorDeleted = "actor.deleted"
)

// Envelope — общая обёртка для всех событий топика.
// Потребитель сначала читает Type, потом разбирает Payload в нужную структуру
type Envelope struct {
	EventID    string          `json:"event_id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

type ActorDeleted struct {
	ActorID int `json:"actor_id"`
}

// NewActorDeleted возвращает ключ и значение сообщения Kafka
func NewActorDeleted(actorID int) (string, []byte, error) {
	payload, err := json.Marshal(ActorDeleted{ActorID: actorID})
	if err != nil {
		return "", nil, fmt.Errorf("сериализация payload: %w", err)
	}

	value, err := json.Marshal(Envelope{
		EventID:    uuid.NewString(),
		Type:       TypeActorDeleted,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
	if err != nil {
		return "", nil, fmt.Errorf("сериализация события: %w", err)
	}

	return strconv.Itoa(actorID), value, nil
}
```

Решения, которые здесь приняты:

- **Один топик на сущность (`actors.events`), тип внутри конверта.** Все события про актёра 10 попадают
  в один топик и идут по порядку: created → updated → deleted. Если сделать по топику на тип,
  порядок между ними не гарантирован.
- **Ключ = id актёра.** Kafka кладёт сообщения с одинаковым ключом в одну партицию, а порядок гарантирован
  **только внутри партиции**. Без ключа «updated» мог бы обработаться после «deleted».
- **`event_id`** нужен потребителю для идемпотентности (раздел 7.6) и для поиска по логам.
- **Только id в payload, без данных актёра.** Это **event notification**. Альтернатива — event-carried state transfer
  (раздел 10).

### 7.2. Проблема двойной записи

Наивная реализация `DeleteActor`:

```go
s.actorRepository.DeleteActor(ctx, id)   // 1. БД
s.producer.Publish(ctx, event)           // 2. Kafka
```

| Что упало | Последствие |
|---|---|
| Kafka после успешного DELETE | актёр удалён, событие потеряно, у фильмов навсегда висят связи на несуществующего актёра |
| Процесс убили между 1 и 2 | то же самое |
| Сначала Kafka, потом БД, и БД упала | movies удалил связи у **живого** актёра |

Общей транзакции у Postgres и Kafka нет. Это проблема **dual write**, и в реальных проектах её решают
паттерном **Transactional Outbox**:

1. В той же транзакции, что и DELETE, пишем событие в таблицу `outbox` той же БД. Одна транзакция — значит, атомарно.
2. Отдельный процесс (**relay**) читает неотправленные строки из `outbox`, публикует их в Kafka и помечает отправленными.

Если Kafka лежит, события копятся в `outbox` и уйдут, когда она поднимется. Ни одно не потеряется.
Цена — **at-least-once**: relay может отправить событие, упасть до пометки «отправлено» и отправить его ещё раз.
Поэтому потребитель обязан быть идемпотентным (7.6).

### 7.3. Миграция outbox

На шаге 3 БД ещё общая, поэтому миграция пока идёт в `migrations/movies` (бывшая `migrations/`).
На шаге 4 она переедет.

```sql
CREATE TABLE golang_kp.outbox (
    id           BIGSERIAL PRIMARY KEY,
    topic        TEXT        NOT NULL,
    key          TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

-- Частичный индекс: только неотправленные. Он маленький, даже когда в таблице миллионы старых строк
CREATE INDEX outbox_unpublished_idx ON golang_kp.outbox (id) WHERE published_at IS NULL;
```

В этой же миграции (или в соседней) удали FK и каскад с `actor_id`:

```sql
ALTER TABLE golang_kp.movie_actors DROP CONSTRAINT movie_actors_actor_id_fkey;
```

Точное имя ограничения посмотри через `\d golang_kp.movie_actors` в psql. FK на `movies` **оставляем**:
обе таблицы в одной БД и в одном сервисе.

### 7.4. actors-service: DeleteActor в транзакции

`internal/core/domain/outbox.go`

```go
package domain

type OutboxMessage struct {
	Topic   string
	Key     string
	Payload []byte
}

func NewOutboxMessage(topic, key string, payload []byte) OutboxMessage {
	return OutboxMessage{Topic: topic, Key: key, Payload: payload}
}
```

Сервис формирует событие:

```go
func (s *ActorService) DeleteActor(ctx context.Context, id int) error {
	key, payload, err := actorsevents.NewActorDeleted(id)
	if err != nil {
		return fmt.Errorf("формирование события: %w", err)
	}

	event := domain.NewOutboxMessage(actorsevents.Topic, key, payload)

	if err = s.actorRepository.DeleteActor(ctx, id, event); err != nil {
		return fmt.Errorf("удаление актера/актрисы: %w", err)
	}

	return nil
}
```

Репозиторий выполняет оба запроса в **одной транзакции**. Транзакции в проекте встречаются впервые:

```go
func (r *ActorRepository) DeleteActor(ctx context.Context, id int, event domain.OutboxMessage) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	// После успешного Commit ничего не делает, при любом раннем return откатывает транзакцию
	defer tx.Rollback(ctx)

	cmdTag, err := tx.Exec(ctx, `DELETE FROM actors WHERE id = $1;`, id)
	if err != nil {
		return fmt.Errorf("удаление актера/актрисы: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("актер/актриса с id='%d': %w", id, core_errors.ErrNotFound)
	}

	_, err = tx.Exec(
		ctx,
		`INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3);`,
		event.Topic,
		event.Key,
		event.Payload,
	)
	if err != nil {
		return fmt.Errorf("запись события в outbox: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("фиксация транзакции: %w", err)
	}

	return nil
}
```

**`defer tx.Rollback(ctx)` — идиома, которую надо запомнить.** Без неё любой `return err` посередине оставит
транзакцию открытой и соединение «зависшим» в пуле.

### 7.5. Core: Kafka-клиент и outbox relay

`internal/core/kafka/config.go`: префикс `KAFKA`, поле `Brokers []string` (`envconfig:"BROKERS" required:"true"`).
envconfig сам разбивает `localhost:9092,localhost:9093` по запятым.

`internal/core/kafka/client.go`

```go
package core_kafka

func NewClient(ctx context.Context, config Config, opts ...kgo.Opt) (*kgo.Client, error) {
	opts = append(opts, kgo.SeedBrokers(config.Brokers...))

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("создание kafka-клиента: %w", err)
	}

	// Как pool.Ping для Postgres: падаем на старте, а не на первом сообщении
	if err = client.Ping(ctx); err != nil {
		client.Close()

		return nil, fmt.Errorf("проверка соединения с kafka: %w", err)
	}

	return client, nil
}
```

Producer-настройки по умолчанию у franz-go уже «продовые»: `acks=all` (запись подтверждена всеми репликами)
и идемпотентный producer (брокер отбрасывает дубли при сетевых ретраях). Явно ничего задавать не нужно.

`internal/core/outbox/relay.go`

```go
package core_outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_pgx_pool "github.com/vasya2314/golang-kp/internal/core/repository/postgres"
	"go.uber.org/zap"
)

type Relay struct {
	pool      *core_pgx_pool.Pool
	kafka     *kgo.Client
	logger    *core_logger.Logger
	interval  time.Duration
	batchSize int
}

func NewRelay(pool *core_pgx_pool.Pool, kafka *kgo.Client, logger *core_logger.Logger) *Relay {
	return &Relay{
		pool:      pool,
		kafka:     kafka,
		logger:    logger,
		interval:  time.Second,
		batchSize: 100,
	}
}

func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.publishBatch(ctx); err != nil {
				// Не выходим: Kafka или БД могли моргнуть, через секунду попробуем снова
				r.logger.Error("отправка outbox", zap.Error(err))
			}
		}
	}
}

func (r *Relay) publishBatch(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE SKIP LOCKED: если запущено несколько реплик сервиса,
	// каждая берёт свои строки и не ждёт чужие блокировки
	rows, err := tx.Query(ctx, `
	SELECT id, topic, key, payload
	FROM outbox
	WHERE published_at IS NULL
	ORDER BY id
	LIMIT $1
	FOR UPDATE SKIP LOCKED;
	`, r.batchSize)
	if err != nil {
		return fmt.Errorf("выборка outbox: %w", err)
	}

	var (
		ids     []int64
		records []*kgo.Record
	)

	for rows.Next() {
		var (
			id         int64
			topic, key string
			payload    []byte
		)
		if err = rows.Scan(&id, &topic, &key, &payload); err != nil {
			rows.Close()

			return fmt.Errorf("сканирование outbox: %w", err)
		}

		ids = append(ids, id)
		records = append(records, &kgo.Record{Topic: topic, Key: []byte(key), Value: payload})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return fmt.Errorf("следующая строка: %w", err)
	}

	if len(records) == 0 {
		return nil
	}

	// Ждём подтверждения от Kafka по каждой записи
	if err = r.kafka.ProduceSync(ctx, records...).FirstErr(); err != nil {
		// Транзакция откатится, строки останутся неотправленными, и следующий тик повторит их.
		// Часть записей могла уже уйти — отсюда at-least-once
		return fmt.Errorf("публикация в kafka: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = ANY($1);`, ids)
	if err != nil {
		return fmt.Errorf("пометка outbox: %w", err)
	}

	return tx.Commit(ctx)
}
```

**Сборка в `app_actors`.** Создай Kafka-клиент через `core_kafka.NewClient(ctx, cfg)`, `defer client.Close()`,
и добавь третью горутину в errgroup: `group.Go(func() error { return relay.Run(groupCtx) })`.

**Самопроверка:**
1. Почему relay держит транзакцию открытой, пока ждёт Kafka? Что было бы, если сначала сделать `UPDATE ... published_at`,
   закоммитить и только потом отправлять?
2. Таблица `outbox` растёт бесконечно. Как бы ты её чистил? (Подсказка: фоновое удаление строк с `published_at` старше N дней.)

### 7.6. movies-service: consumer

**Семантика доставки.** Мы коммитим offset (отметку «прочитал до сюда») **после** обработки сообщения.
Если процесс упадёт между обработкой и коммитом, после рестарта сообщение придёт снова.
Это **at-least-once**, как и у relay. Значит, обработчик должен выдерживать повтор.

Удобно, что `DELETE FROM movie_actors WHERE actor_id = $1` идемпотентен сам по себе: повторный запуск удалит 0 строк.
Для неидемпотентных обработчиков (например, «увеличить счётчик») нужен **inbox**: таблица `processed_events(event_id PK)`,
в которую в одной транзакции с изменением пишется `event_id`. При повторе вставка падает на конфликте, и событие пропускается.

**Сервис и репозиторий.** `RemoveActorLinks(ctx, actorID int) error` → `DELETE FROM movie_actors WHERE actor_id = $1;`.
Индекс по `actor_id` у тебя уже есть: вот и пригодился.

`internal/modules/movies/transport/kafka/consumer.go`. Kafka — ещё один **transport** (входящий адаптер), как HTTP:

```go
package movie_transport_kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	actorsevents "github.com/vasya2314/golang-kp/pkg/events/actors"
	"go.uber.org/zap"
)

const (
	ConsumerGroup = "movies-service"
	maxBackoff    = 10 * time.Second
)

type MovieService interface {
	RemoveActorLinks(ctx context.Context, actorID int) error
}

type ActorEventsConsumer struct {
	client       *kgo.Client
	movieService MovieService
	logger       *core_logger.Logger
}

func NewActorEventsConsumer(
	client *kgo.Client,
	movieService MovieService,
	logger *core_logger.Logger,
) *ActorEventsConsumer {
	return &ActorEventsConsumer{client: client, movieService: movieService, logger: logger}
}

func (c *ActorEventsConsumer) Run(ctx context.Context) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			c.logger.Error(
				"ошибка чтения из kafka",
				zap.String("topic", topic),
				zap.Int32("partition", partition),
				zap.Error(err),
			)
		})

		fetches.EachRecord(func(record *kgo.Record) {
			c.handleWithRetry(ctx, record)
		})

		// Если нас остановили посреди пачки, не коммитим: необработанное придёт снова после рестарта
		if ctx.Err() != nil {
			return nil
		}

		if err := c.client.CommitUncommittedOffsets(ctx); err != nil {
			c.logger.Error("коммит offset-ов", zap.Error(err))
		}
	}
}

// Повторяем, пока не получится: временная ошибка (БД моргнула) не должна терять событие
func (c *ActorEventsConsumer) handleWithRetry(ctx context.Context, record *kgo.Record) {
	backoff := 100 * time.Millisecond

	for {
		err := c.handle(ctx, record)
		if err == nil || ctx.Err() != nil {
			return
		}

		c.logger.Warn(
			"обработка события, повтор",
			zap.Int64("offset", record.Offset),
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *ActorEventsConsumer) handle(ctx context.Context, record *kgo.Record) error {
	var envelope actorsevents.Envelope
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		// Битое сообщение не починится повтором: ретраить бесполезно, иначе застрянем навсегда.
		// В проде такое кладут в DLQ (раздел 10)
		c.logger.Error("невалидное сообщение, пропускаем", zap.Int64("offset", record.Offset), zap.Error(err))

		return nil
	}

	log := c.logger.With(
		zap.String("event_id", envelope.EventID),
		zap.String("event_type", envelope.Type),
	)
	ctx = core_logger.ToContext(ctx, log)

	switch envelope.Type {
	case actorsevents.TypeActorDeleted:
		var payload actorsevents.ActorDeleted
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			log.Error("невалидный payload, пропускаем", zap.Error(err))

			return nil
		}

		if err := c.movieService.RemoveActorLinks(ctx, payload.ActorID); err != nil {
			return fmt.Errorf("удаление связей актера %d: %w", payload.ActorID, err)
		}

		log.Debug("связи актера удалены", zap.Int("actor_id", payload.ActorID))

	default:
		// Неизвестный тип — не ошибка: actors-service мог добавить новое событие,
		// а мы его пока не обрабатываем. Это и есть forward compatibility
	}

	return nil
}
```

Две категории ошибок, которые важно различать:

| Ошибка | Пример | Что делать |
|---|---|---|
| **Временная** | БД недоступна, таймаут | повторять с backoff, offset не двигать |
| **Постоянная** | битый JSON | повтор не поможет: залогировать и пропустить (или в DLQ) |

Если перепутать и ретраить постоянную ошибку, consumer застрянет на одном сообщении навсегда (**poison pill**),
а за ним встанет вся партиция.

**Сборка в `app_movies`:**

```go
kafkaClient, err := core_kafka.NewClient(ctx, core_kafka.NewConfigMust(),
	kgo.ConsumerGroup(movie_transport_kafka.ConsumerGroup),
	kgo.ConsumeTopics(actorsevents.Topic),
	// Коммитим сами, после обработки. Автокоммит коммитит по таймеру — и может отметить
	// то, что ещё не обработано, а при падении такое событие потеряется
	kgo.DisableAutoCommit(),
)
// ...
defer kafkaClient.Close()

group.Go(func() error { return httpServer.Run(groupCtx) })
group.Go(func() error { return consumer.Run(groupCtx) })
```

**Consumer group.** Все реплики movies-service с одним `ConsumerGroup` делят партиции между собой:
каждое сообщение обработает **одна** реплика. Если завтра появится search-service, он заведёт свою группу
и получит **все** те же события независимо от movies. Это главное отличие Kafka от очереди задач.

### 7.7. Вернёмся к вопросу из 6.7

Почему `BatchGetActors` может вернуть меньше актёров, чем было id?

```
t1  movies: AddMovieActor → gRPC GetActor(10) → OK
t2  actors: DELETE actor 10 + outbox
t3  movies: INSERT movie_actors (1, 10)          ← актёр уже удалён!
t4  movies consumer: actor.deleted(10) → DELETE связи   ← подчистили
```

Между t2 и t4 (обычно доли секунды, но при лежащей Kafka — часы) в `movie_actors` есть id несуществующего актёра.
Это **eventual consistency**: система приходит к согласованному состоянию не мгновенно, а **в итоге**.

Обрати внимание: гонка t1→t3 закрывается тем же событием, потому что DELETE в консьюмере сработает после INSERT.
Поэтому `GetMovieActors` спокойно возвращает только найденных актёров. Расхождение — нормальное временное состояние,
а не ошибка.

**В этом и состоит цена разделения:** раньше FK гарантировал целостность всегда, теперь только «в итоге».

## 8. Шаг 4 — раздельные БД

К этому моменту movies-service не читает и не пишет `actors` и `outbox`. Осталось физически перенести данные.

### 8.1. Миграции

```
migrations/movies/   ← всё, что было (000001…000006+)
migrations/actors/
  000001_init.up.sql     ← CREATE SCHEMA golang_kp; CREATE TABLE actors (как в 000003)
  000002_outbox.up.sql   ← outbox из 7.3
```

И новая миграция в `movies`: `DROP TABLE golang_kp.outbox; DROP TABLE golang_kp.actors;`.
**Накатывай её последней**, после переноса данных.

`Makefile`: параметризуем миграции именем сервиса. Здесь удобен приём с переменной в имени переменной:

```make
DB_movies := ${POSTGRES_DB}
DB_actors := golang_kp_actors

# make migrate-up svc=actors
migrate-up:
	@make migrate-action svc=$(svc) action=up n=$(n)

migrate-action:
	# ...как было, только:
	#   -path /migrations/$(svc)
	#   .../$(DB_$(svc))?sslmode=disable...

db-create-actors:
	@docker compose exec -T golang-kp-postgres \
		psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -c "CREATE DATABASE ${DB_actors};"
```

`migrate-ensure-schema` тоже должен работать с `-d $(DB_$(svc))`. `migrate-create` — с `-dir /migrations/$(svc)`.

### 8.2. Перенос данных

```bash
# 1. Останови actors-service и убедись, что outbox пуст (relay всё отправил):
#    SELECT count(*) FROM golang_kp.outbox WHERE published_at IS NULL;  → 0

make db-create-actors
make migrate-up svc=actors

# 2. Перелить данные
docker compose exec -T golang-kp-postgres \
  pg_dump -U golang_kp -d golang_kp --data-only -t golang_kp.actors \
| docker compose exec -T golang-kp-postgres \
  psql -U golang_kp -d golang_kp_actors

# 3. Счётчик id — иначе следующий INSERT получит id=1 и упадёт на PRIMARY KEY
#    (в psql к golang_kp_actors)
SELECT setval('golang_kp.actors_id_seq', (SELECT max(id) FROM golang_kp.actors));
```

4. В `run-actors` поменяй `POSTGRES_DB=golang_kp_actors`, запусти actors-service и проверь ручки.
5. Только теперь `make migrate-up svc=movies`: удаление `actors` и `outbox` из старой БД.

В проде такой перенос делают без остановки сервиса: двойная запись в старую и новую БД, фоновое копирование,
сверка, переключение чтения. Или через CDC (Debezium). Для учебного проекта хватит остановки на минуту.

**Почему одна Postgres, а не две.** Для изоляции важна **логическая** граница: своя БД (или хотя бы своя схема и свой
пользователь), к которой у другого сервиса нет доступа. Будет ли она на отдельном сервере — вопрос нагрузки и денег.
В проде часто так и делают: один кластер Postgres, много баз.

## 9. Проверка всего вместе

```bash
make env-up && make kafka-create-topics
make run-actors     # терминал 1
make run-movies     # терминал 2
make kafka-tail topic=actors.events   # терминал 3
```

Сценарии. Каждый показывает, **зачем** нужна соответствующая часть:

| Сценарий | Ожидание | Что демонстрирует |
|---|---|---|
| Привязать актёра 999 к фильму | 404 | gRPC вместо FK |
| `GET /movies/1/actors` | актёры с данными | BatchGetActors вместо JOIN |
| Удалить актёра | в терминале 3 появилось событие, связи у фильмов исчезли | Kafka вместо CASCADE |
| Останови actors-service, `GET /movies/1/actors` | **503**, movies-service жив | синхронная зависимость, ретраи, FromStatus |
| Останови **movies-service**, удали актёра, запусти movies | связи удалятся после старта | Kafka хранит события, пока получатель лежит |
| `docker compose stop golang-kp-kafka`, удали актёра | **204**, в `outbox` строка с `published_at IS NULL` | outbox: удаление не зависит от Kafka |
| `docker compose start golang-kp-kafka` | через секунду событие в топике, связи удалены | relay дослал |
| Ctrl+C у movies-service | в логе «Shutting down» у HTTP и consumer, без паник | graceful shutdown через errgroup |

Сценарии 4–7 — самое ценное в разборе. Обязательно прогони их руками.

## 10. Как это делают в реальных проектах

### Контракты

- **Proto-файлы в отдельном репозитории** (или в Buf Schema Registry), если сервисы в разных репозиториях.
  `buf breaking --against '.git#branch=main'` в CI запрещает ломать контракт в PR. Это стандарт, подключи его сразу.
- **События тоже по схеме:** Protobuf или Avro + **Schema Registry** (Confluent, Redpanda, Apicurio).
  Producer не сможет отправить сообщение, несовместимое со схемой. JSON без схемы, как у нас, тоже встречается,
  но это источник ошибок «поле переименовали — потребитель молча получает пустоту».
- Правила эволюции: поля только **добавлять**, удалять только через `reserved`, потребитель **игнорирует** незнакомое.

### Данные между сервисами

- **Вместо gRPC-вызова — локальная копия.** actors-service публикует `actor.created/updated/deleted` с **полными данными**
  (event-carried state transfer). movies-service хранит у себя таблицу `actors_view(id, first_name, last_name)`
  и снова делает JOIN локально. Плюс: movies не зависит от доступности actors. Минус: данные могут отставать,
  и это ещё один consumer. Крупные компании часто приходят именно к этому.
- **Outbox через CDC.** Вместо polling-relay используют **Debezium**: он читает WAL Postgres и сам публикует строки
  outbox в Kafka. Меньше нагрузки на БД и меньше задержка. Polling-relay, как у нас, тоже нормальный продовый вариант
  для умеренных объёмов.
- **Транзакции через Unit of Work / transaction manager** (например, `avito-tech/go-transaction-manager`).
  Сервис открывает транзакцию через `ctx`, и любые методы репозиториев внутри неё работают в ней.
  Это лучше, чем передавать `OutboxMessage` в `DeleteActor`, как сделали мы.
- **Саги** для бизнес-операций из нескольких шагов в разных сервисах (заказ → оплата → доставка) с компенсациями
  при сбое. Сейчас нам не нужно, но термин стоит знать.

### Надёжность

- **Deadline на каждый вызов** (у нас 2 с в service config) и его передача дальше через `ctx`.
  gRPC сам передаёт оставшееся время следующему сервису.
- **Ретраи только идемпотентных операций** и только на `UNAVAILABLE`. Плюс **circuit breaker** (`sony/gobreaker`):
  если actors-service лежит, не долбить его запросами, а сразу отвечать 503.
- **Health checks:** `grpc.health.v1` для gRPC, `/healthz` и `/readyz` для HTTP. Kubernetes по ним решает,
  слать ли трафик и перезапускать ли под.
- **DLQ и retry-топики.** Битое сообщение не просто логируют, а отправляют в `actors.events.dlq`,
  чтобы потом разобрать или переотправить. Временные ошибки иногда уводят в `actors.events.retry.1m`,
  чтобы не блокировать партицию.
- **Ребалансировка consumer group.** У franz-go для коммита без гонок при ребалансе есть `kgo.BlockRebalanceOnPoll()`.
  Идемпотентный обработчик делает редкие дубли безопасными, поэтому у нас это не критично.

### Наблюдаемость

- **Распределённый трейсинг (OpenTelemetry).** Один `trace_id` проходит HTTP → gRPC → Kafka (через заголовки записи) → consumer.
  Без него в микросервисах невозможно понять, где тормозит запрос. Твой `RequestID`-middleware — первый шаг:
  его можно передавать в gRPC через `metadata`, а в Kafka через `record.Headers`.
- **Метрики:** RED (rate, errors, duration) на каждый метод и **consumer lag**, то есть насколько consumer отстаёт
  от топика. Lag — главная метрика любого Kafka-консьюмера.

### Инфраструктура

- **API Gateway** (Envoy, Traefik, nginx, Kong): клиент видит один адрес, а `/api/v1/movies` и `/api/v1/actors`
  маршрутизируются в разные сервисы. Там же — аутентификация, rate limit, TLS. Упражнение ниже.
- **gRPC внутри, REST/JSON снаружи.** Типичная схема: браузеру удобен JSON, а между сервисами важны контракт и скорость.
- **mTLS между сервисами** (часто через service mesh: Istio, Linkerd) вместо `insecure.NewCredentials()`.
- **Топики создаёт инфраструктура** (Terraform, Strimzi в k8s), а не приложение. Отсюда `AUTO_CREATE_TOPICS_ENABLE=false`.
- В проде у топика **replication factor 3** и `min.insync.replicas=2`: переживает падение одного брокера без потери данных.

## 11. Упражнения

1. **API Gateway.** Добавь в compose nginx на `:8080`: `location /api/v1/movies` → `host.docker.internal:5055`,
   `location /api/v1/actors` → `:5056`. Клиент снова видит один адрес.
2. **`actor.updated` + локальная копия.** Публикуй событие при `PatchActor` с полными данными, храни в movies
   таблицу `actors_view` и сделай `GetMovieActors` без gRPC. Сравни поведение при остановленном actors-service.
   Как обработать событие, пришедшее не по порядку? (Подсказка: поле `version` у актёра.)
3. **Request ID через границы.** Передай `X-Request-ID` из HTTP-запроса в gRPC metadata (client interceptor)
   и в заголовки Kafka-записи (через outbox — добавь колонку `headers JSONB`). Найди один запрос в логах обоих сервисов.
4. **DLQ.** Невалидные сообщения публикуй в `actors.events.dlq` вместо простого лога.
5. **Health check.** Зарегистрируй `health.NewServer()` и проверь его через `grpcurl ... grpc.health.v1.Health/Check`.
6. **Чистка outbox.** Раз в час удаляй строки с `published_at < now() - interval '7 days'`.
7. **Постеры из `posters.md`, раздел 13.4:** событие `PosterUploaded` → воркер превью. Теперь у тебя есть всё для этого:
   outbox, relay, consumer.

## Итог: что было «бесплатно» и чем мы за это заплатили

| В монолите | В сервисах | Цена |
|---|---|---|
| FK | gRPC `GetActor` | сетевой вызов, 503 при недоступности, гонка (7.7) |
| JOIN | gRPC `BatchGetActors` | второй запрос, обязательный batch |
| `ON DELETE CASCADE` | outbox → Kafka → consumer | таблица outbox, relay, идемпотентность, eventual consistency |
| одна транзакция | транзакция на сервис + события | at-least-once, дубли, порядок только внутри партиции |
| один процесс | два процесса + Kafka | больше инфраструктуры, логи в двух местах, трейсинг |

Если на собеседовании спросят «почему вы распилили монолит», правильный ответ начинается с проблемы,
которую это решило, а не с технологий. Если сможешь объяснить эту таблицу, значит, разобрался.
