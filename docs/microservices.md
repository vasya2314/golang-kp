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

**Самопроверка:** `BatchGetActors` вернул 2 актёра из 3 id. Когда так бывает, и почему это не ошибка? (Ответ в разделе 7.19.)

## 7. Шаг 3 — Kafka и outbox: удаляем связи при удалении актёра

### 7.1. Что мы делаем и зачем, простыми словами

**Как сейчас.** Ты удаляешь актёра 10 → Postgres сам удаляет все строки `movie_actors`, где `actor_id = 10`.
Это делает `ON DELETE CASCADE` у внешнего ключа. Работает только потому, что обе таблицы лежат в одной базе.

**Что сломается.** На шаге 4 таблица `actors` уедет в другую базу. Внешний ключ между базами сделать нельзя,
значит, и каскада не будет. Актёр удалится, а связи с фильмами останутся висеть.

**Что нужно.** Когда actors-service удаляет актёра, movies-service должен об этом **узнать** и удалить связи у себя.

**Почему не gRPC.** Можно было бы сделать, чтобы actors-service после удаления вызывал movies-service по gRPC:
«удали связи актёра 10». Но тогда:
- actors-service должен знать про movies-service. Завтра появится search-service, которому тоже важно удаление, —
  придётся снова менять actors-service;
- если movies-service в этот момент выключен, вызов не пройдёт и информация потеряется.

**Решение — Kafka.** Kafka — это **общий журнал сообщений** между сервисами. Похоже на ленту объявлений:
- actors-service пишет в ленту объявление: «актёр 10 удалён». Кто его прочитает, ему неважно;
- movies-service читает ленту и на каждое такое объявление удаляет связи у себя;
- если movies-service выключен, объявление **никуда не пропадает**: Kafka хранит его на диске,
  и movies-service прочитает его, когда запустится.

Остаётся одна проблема: как надёжно записать объявление в Kafka. Для этого нужен **outbox** — о нём в 7.3.

### 7.2. Словарь

Все новые слова этого и следующих разделов. Возвращайся сюда, если в тексте встретил непонятное.

**Kafka**

| Термин | Простыми словами | У нас |
|---|---|---|
| **Kafka** | Отдельный сервер, который принимает сообщения, хранит их на диске и отдаёт тем, кто их читает | контейнер `golang-kp-kafka` |
| **Брокер** | Один сервер Kafka. В проде их несколько, у нас один | `localhost:9092` |
| **Событие (event)** | Сообщение о том, что **уже произошло**, в прошедшем времени: «актёр удалён», а не «удали связи» | `actor.deleted` |
| **Сообщение / запись (record)** | То, что лежит в Kafka: **ключ** + **значение** (байты, у нас JSON) | ключ `"10"`, значение — JSON события |
| **Топик (topic)** | Именованная лента сообщений, как таблица в БД. Пишут и читают по имени | `actors.events` |
| **Партиция (partition)** | Топик разбит на несколько частей, чтобы его можно было читать параллельно. **Порядок сообщений гарантирован только внутри одной партиции** | у топика 3 партиции |
| **Ключ сообщения** | По ключу Kafka выбирает партицию: одинаковый ключ → всегда одна и та же партиция → сообщения про одного актёра идут строго по порядку | id актёра |
| **Producer** | Тот, кто пишет в Kafka | relay в actors-service |
| **Consumer** | Тот, кто читает из Kafka | consumer в movies-service |
| **Offset** | Порядковый номер сообщения внутри партиции: 0, 1, 2… Как номер строки | — |
| **Коммит offset-а** | Consumer сообщает Kafka: «я обработал всё до сообщения N». После перезапуска он продолжит с N+1. Похоже на закладку в книге | `CommitUncommittedOffsets` |
| **Consumer group** | Имя «команды читателей». Все реплики movies-service — одна группа, и каждое сообщение достанется **одной** из них. Другой сервис с другим именем группы получит **все** сообщения независимо | группа `movies-service` |

**Надёжная доставка**

| Термин | Простыми словами | У нас |
|---|---|---|
| **Транзакция** | Несколько SQL-запросов, которые выполняются «всё или ничего». Упал любой — откатываются все | DELETE актёра + INSERT в outbox |
| **Dual write** | «Двойная запись»: одно действие пишет в **два** разных хранилища (Postgres и Kafka). Общей транзакции у них нет, поэтому одна запись может пройти, а другая нет | проблема из 7.3 |
| **Outbox** («исходящие») | Обычная таблица в Postgres, куда мы кладём событие **в той же транзакции**, что и основное изменение. Как папка «Исходящие» в почте: письмо сначала попадает туда, а отправляется потом | таблица `outbox` |
| **Relay** («передатчик») | Фоновая горутина: раз в секунду берёт неотправленные строки из `outbox`, отправляет в Kafka и помечает отправленными | `core_outbox.Relay` |
| **At-least-once** («хотя бы один раз») | Гарантия доставки: сообщение точно дойдёт, но **может дойти дважды**. Альтернатива «ровно один раз» на практике почти недостижима | и relay, и consumer |
| **Идемпотентность** | Операцию можно повторить сколько угодно раз, а результат будет как от одного раза. `DELETE ... WHERE actor_id = 10` идемпотентен: второй раз удалит 0 строк. «Увеличь счётчик на 1» — нет | обработчик в consumer |
| **Eventual consistency** («согласованность в итоге») | Данные двух сервисов какое-то время могут расходиться, но **в итоге** придут в согласие | связи удаляются не мгновенно, а через доли секунды |
| **Backoff** | Пауза между повторами, которая растёт: 0.1s, 0.2s, 0.4s… Чтобы не долбить упавшую БД | `handleWithRetry` |
| **Poison pill** («отравленное сообщение») | Сообщение, которое никогда не обработается (например, битый JSON). Если его бесконечно повторять, consumer застрянет и не прочитает ничего после него | пропускаем с логом |
| **DLQ (dead letter queue)** | Отдельный топик для «отравленных» сообщений: их не теряют, а откладывают, чтобы человек потом разобрался | упражнение 4 |
| **Inbox** | Таблица `processed_events(event_id)` у получателя: «эти события я уже обработал». Нужна, если обработчик **не** идемпотентный | у нас не нужна |

**Из раздела 10 (для общего развития)**

| Термин | Простыми словами |
|---|---|
| **CDC (change data capture)** | Программа читает журнал изменений Postgres (WAL) и сама пересылает изменения в Kafka. Пример — **Debezium**. Заменяет наш relay |
| **Сага (saga)** | Бизнес-операция из нескольких шагов в разных сервисах, где при сбое на шаге 3 запускаются «отменяющие» действия для шагов 1–2 |
| **Circuit breaker** («предохранитель») | Если сервис-зависимость много раз подряд не ответил, перестаём его вызывать на какое-то время и сразу отвечаем ошибкой |
| **Schema Registry** | Сервер, который хранит схемы сообщений Kafka и не даёт отправить сообщение, не подходящее под схему |

### 7.3. Проблема: почему нельзя просто «удалить и отправить»

Первое, что приходит в голову:

```go
s.actorRepository.DeleteActor(ctx, id)   // 1. удалили в Postgres
s.producer.Publish(ctx, event)           // 2. отправили в Kafka
```

Между строкой 1 и строкой 2 может случиться что угодно:

| Что пошло не так | Что получится |
|---|---|
| Kafka недоступна, строка 2 вернула ошибку | актёр удалён, события нет. У фильмов **навсегда** остались связи на несуществующего актёра |
| Процесс убили (деплой, OOM) между строками 1 и 2 | то же самое |
| Поменяли порядок: сначала Kafka, потом БД, и БД упала | movies-service удалил связи у **живого** актёра |

Это и есть **dual write**: пишем в два места, а «всё или ничего» для двух разных систем не существует.

### 7.4. Решение: outbox

Идея: **не отправлять в Kafka сразу**. Положить событие в таблицу `outbox` той же базы,
**в одной транзакции** с удалением. Тогда либо удалилось и записалось событие, либо ничего.
А отправкой в Kafka займётся отдельная горутина.

```
actors-service
┌───────────────────────────────────────────────────────────┐
│  HTTP DELETE /actors/10                                    │
│     └─ транзакция:  DELETE FROM actors WHERE id = 10       │
│                     INSERT INTO outbox (событие)           │  ← либо оба, либо ничего
│                                                            │
│  relay (раз в секунду):                                    │
│     SELECT * FROM outbox WHERE published_at IS NULL        │
│     → отправить в Kafka → дождаться «принято»              │
│     → UPDATE outbox SET published_at = now()               │
└───────────────────────────────┬────────────────────────────┘
                                ▼
                    Kafka, топик actors.events
                                │
movies-service                  ▼
┌───────────────────────────────────────────────────────────┐
│  consumer:                                                 │
│     прочитать сообщение → DELETE FROM movie_actors         │
│                           WHERE actor_id = 10              │
│     → закоммитить offset («обработал»)                     │
└───────────────────────────────────────────────────────────┘
```

Что это даёт:
- **Kafka лежит** → удаление актёра всё равно проходит, события копятся в `outbox` и уйдут, когда Kafka поднимется;
- **movies-service лежит** → события копятся в Kafka и обработаются после его запуска;
- **ни одно событие не теряется**.

Цена — **дубли**. Relay может отправить событие в Kafka и упасть, не успев поставить `published_at`.
После перезапуска он отправит это событие ещё раз. То же с consumer: обработал, упал до коммита offset-а — получит
сообщение снова. Поэтому обработчик в movies-service обязан быть **идемпотентным**. У нас это так: повторный
`DELETE ... WHERE actor_id = 10` просто удалит 0 строк.

### 7.5. План

Порядок выбран так, чтобы после каждого шага всё работало и можно было проверить результат.

| # | Что делаем | Где | Как проверить |
|---|---|---|---|
| 1 | Поднять Kafka, создать топик, добавить переменную | `Makefile`, `.env` | топик виден в `kafka-topics.sh --describe` |
| 2 | Контракт события | `pkg/events/actors/events.go` | **уже сделано** |
| 3 | Миграция: таблица `outbox` | `migrations/` | `\d golang_kp.outbox` в psql |
| 4 | Тип `OutboxMessage` | `internal/core/domain/outbox.go` | компилируется |
| 5 | `DeleteActor` в транзакции | репозиторий actors | — |
| 6 | Сервис формирует событие | сервис actors | удаление актёра → строка в `outbox` |
| 7 | Конфиг и клиент Kafka | `internal/core/kafka/` | — |
| 8 | Relay | `internal/core/outbox/relay.go` | — |
| 9 | Запустить relay в actors-service | `internal/app/actors/app.go` | событие в `make kafka-tail`, `published_at` заполнен |
| 10 | `RemoveActorLinks` | репозиторий и сервис movies | — |
| 11 | Consumer | `internal/modules/movies/transport/kafka/` | — |
| 12 | Запустить consumer в movies-service | `internal/app/movies/app.go` | в логах «связи актера удалены» |
| 13 | Миграция: убрать FK `movie_actors → actors` | `migrations/` | удаление работает **только** через Kafka |
| 14 | Прогнать сценарии | — | таблица в 7.20 |

---

### 7.6. Шаг 1 — Kafka, топик, переменная окружения

Kafka уже описана в `docker-compose.yaml` (раздел 4), но `make env-up` её не запускает.

**Шаг 1.1.** В `Makefile` добавь `golang-kp-kafka` в `env-up` и `env-down`:

```make
env-up:
	@docker compose up -d golang-kp-postgres golang-kp-s3 golang-kp-kafka

env-down:
	@docker compose down golang-kp-postgres golang-kp-s3 golang-kp-kafka
```

**Шаг 1.2.** Запусти и создай топик:

```bash
make env-up
make kafka-create-topics
```

`kafka-create-topics` создаёт топик `actors.events` с 3 партициями. Kafka настроена так, что сама топики
**не** создаёт (`KAFKA_AUTO_CREATE_TOPICS_ENABLE=false`): в проде топики заводит инфраструктура, а не приложение.

**Шаг 1.3.** Проверь:

```bash
docker compose exec golang-kp-kafka /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --describe --topic actors.events
```

Должны быть три строки `Partition: 0`, `1`, `2`.

**Шаг 1.4.** В `.env` добавь адрес брокера (через запятую можно перечислить несколько):

```
KAFKA_BROKERS=localhost:9092
```

**Шаг 1.5.** Зависимость `franz-go` уже есть в `go.mod`, но с пометкой `// indirect`. После того как появится код,
который её использует (шаг 7), выполни `go mod tidy`.

### 7.7. Шаг 2 — контракт события (уже сделано)

Файл `pkg/events/actors/events.go` у тебя уже есть. Разберём, что в нём и зачем.

**Почему `pkg/`, а не `internal/`.** Это контракт между сервисами, как `.proto` для gRPC.
Его импортируют оба: actors-service, чтобы создать событие, и movies-service, чтобы прочитать.

**Конверт (`Envelope`).** Каждое сообщение в топике — это JSON такого вида:

```json
{
  "event_id": "5f0c…",
  "type": "actor.deleted",
  "occurred_at": "2026-10-15T12:00:00Z",
  "payload": { "actor_id": 10 }
}
```

- `type` говорит, **что** случилось. Consumer сначала смотрит на `type` и только потом разбирает `payload`
  в нужную структуру. Так в одном топике могут жить разные события: сегодня `actor.deleted`, завтра `actor.updated`.
- `event_id` — уникальный id события (UUID). Нужен, чтобы найти событие в логах обоих сервисов
  и чтобы при необходимости отсеять дубли (inbox).
- `occurred_at` — когда это случилось.
- `payload` — данные события. Тип `json.RawMessage` значит «сырые байты JSON, разберём потом».

**`NewActorDeleted(actorID)`** возвращает:
- **ключ** `"10"` — id актёра строкой. Все события про актёра 10 попадут в одну партицию и будут обработаны по порядку;
- **значение** — конверт в JSON.

**Почему в payload только id.** Получателю этого достаточно. Такой стиль называется «уведомление о событии».
Альтернатива — класть в событие все данные актёра (раздел 10, «Данные между сервисами»).

### 7.8. Шаг 3 — миграция: таблица outbox

На этом шаге база ещё общая, поэтому миграция ложится в обычную папку `migrations/`.

**Шаг 3.1.** Создай файлы миграции:

```bash
make migrate-create seq=add_outbox
```

Появятся `migrations/000006_add_outbox.up.sql` и `migrations/000006_add_outbox.down.sql`.

**Шаг 3.2.** `000006_add_outbox.up.sql`:

```sql
CREATE TABLE golang_kp.outbox (
    id           BIGSERIAL PRIMARY KEY,
    topic        TEXT        NOT NULL,
    key          TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX outbox_unpublished_idx ON golang_kp.outbox (id) WHERE published_at IS NULL;
```

| Колонка | Зачем |
|---|---|
| `id` | порядковый номер. Relay отправляет события в порядке `id`, то есть в порядке создания |
| `topic` | в какой топик Kafka отправить. Одна таблица `outbox` на весь сервис, а топики могут быть разными |
| `key` | ключ сообщения Kafka (id актёра) |
| `payload` | значение сообщения: JSON-конверт из шага 2 |
| `created_at` | когда событие создано. Удобно при отладке: «почему событие висит неотправленным уже час?» |
| `published_at` | `NULL` — ещё не отправлено, дата — когда отправлено. По этой колонке relay понимает, что брать |

**Индекс `WHERE published_at IS NULL`** — частичный: в него попадают только неотправленные строки.
Relay каждую секунду ищет именно их. Даже когда в таблице миллион старых отправленных строк, индекс остаётся крошечным.

**Шаг 3.3.** `000006_add_outbox.down.sql`:

```sql
DROP TABLE golang_kp.outbox;
```

**Шаг 3.4.** Накати и проверь:

```bash
make migrate-up
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp -c '\d golang_kp.outbox'
```

Внешний ключ `movie_actors → actors` пока **не трогаем**. Пока он есть, каскад продолжает работать,
и система не ломается, даже если Kafka-часть ещё не дописана. Уберём его последним, на шаге 13.

### 7.9. Шаг 4 — тип OutboxMessage

Сервису нужно передать событие в репозиторий. Заведём для этого маленькую структуру.

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

Это ровно три поля, которые потом лягут в колонки `topic`, `key`, `payload` таблицы `outbox`.

### 7.10. Шаг 5 — репозиторий: DeleteActor в транзакции

До сих пор в проекте каждый метод репозитория выполнял **один** SQL-запрос через `r.pool.Exec`.
Теперь нужно **два** запроса, и они должны пройти вместе или не пройти вообще. Для этого — транзакция.

**Шаг 5.1.** Перепиши `internal/modules/actors/repository/postgres/delete_actor.go`:

```go
package actor_postgres_repository

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *ActorRepository) DeleteActor(ctx context.Context, id int, event domain.OutboxMessage) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	// 1. Открываем транзакцию. Все запросы ниже идут через tx, а не через r.pool
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	// 2. Страховка: если выйдем из функции до Commit (любой return err), транзакция откатится.
	//    После успешного Commit этот Rollback ничего не делает
	defer tx.Rollback(ctx)

	// 3. Удаляем актёра
	cmdTag, err := tx.Exec(ctx, `DELETE FROM actors WHERE id = $1;`, id)
	if err != nil {
		return fmt.Errorf("удаление актера/актрисы: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("актер/актриса с id='%d': %w", id, core_errors.ErrNotFound)
	}

	// 4. Кладём событие в outbox — в той же транзакции
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

	// 5. Фиксируем. Только здесь изменения становятся видны остальным
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("фиксация транзакции: %w", err)
	}

	return nil
}
```

Как это работает:
- до `Commit` никто, кроме нас, не видит ни удаления, ни новой строки в `outbox`;
- актёра не нашли → `return` → `defer tx.Rollback` откатывает → в `outbox` ничего не попало. Это правильно:
  события о том, чего не было, быть не должно;
- INSERT упал → откат → актёр **не** удалён, пользователь получит 500 и может повторить.

**`defer tx.Rollback(ctx)` сразу после `Begin` — идиома, которую надо запомнить.** Без неё любой ранний `return`
оставит транзакцию открытой, а соединение — занятым в пуле. Через несколько таких ошибок пул кончится и сервис встанет.

**Шаг 5.2.** В `internal/modules/actors/service/service.go` поправь интерфейс `ActorRepository`:

```go
	DeleteActor(
		ctx context.Context,
		id int,
		event domain.OutboxMessage,
	) error
```

### 7.11. Шаг 6 — сервис формирует событие

Перепиши `internal/modules/actors/service/delete_actor.go`:

```go
package actor_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
	actors_events "github.com/vasya2314/golang-kp/pkg/events/actors"
)

func (s *ActorService) DeleteActor(ctx context.Context, id int) error {
	key, payload, err := actors_events.NewActorDeleted(id)
	if err != nil {
		return fmt.Errorf("формирование события: %w", err)
	}

	event := domain.NewOutboxMessage(actors_events.Topic, key, payload)

	if err = s.actorRepository.DeleteActor(ctx, id, event); err != nil {
		return fmt.Errorf("удаление актера/актрисы: %w", err)
	}

	return nil
}
```

Почему событие собирает сервис, а не репозиторий: «при удалении актёра надо сообщить миру» — это бизнес-правило.
Репозиторий только сохраняет то, что ему дали.

HTTP-хендлер `DeleteActor` менять не нужно: сигнатура сервиса осталась прежней.

**Проверка.** Запусти actors-service (`make run-actors`), создай тестового актёра и удали его:

```bash
curl -X DELETE localhost:5056/api/v1/actors/<id>

docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp \
  -c 'SELECT id, topic, key, payload, published_at FROM golang_kp.outbox;'
```

Должна появиться строка с `published_at` = пусто. Отправлять её пока некому — это следующий шаг.

### 7.12. Шаг 7 — конфиг и клиент Kafka

Делаем по аналогии с Postgres: конфиг из env и функция, которая создаёт клиент и проверяет соединение.

**Шаг 7.1.** `internal/core/kafka/config.go`:

```go
package core_kafka

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// envconfig сам разбивает "host1:9092,host2:9092" по запятым
	Brokers []string `envconfig:"BROKERS" required:"true"`
}

func NewConfig() (Config, error) {
	var config Config

	err := envconfig.Process("KAFKA", &config)
	if err != nil {
		return Config{}, fmt.Errorf("обработка переменных окружения: %w", err)
	}

	return config, nil
}

func NewConfigMust() Config {
	config, err := NewConfig()
	if err != nil {
		err = fmt.Errorf("получение конфигурации kafka: %w", err)
		panic(err)
	}

	return config
}
```

**Шаг 7.2.** `internal/core/kafka/client.go`:

```go
package core_kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// opts — дополнительные настройки. Producer-у (actors) они не нужны,
// а consumer-у (movies) нужно сказать, какую группу и какие топики читать
func NewClient(ctx context.Context, config Config, opts ...kgo.Opt) (*kgo.Client, error) {
	opts = append(opts, kgo.SeedBrokers(config.Brokers...))

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("создание kafka-клиента: %w", err)
	}

	// Как pool.Ping для Postgres: если Kafka недоступна, падаем на старте, а не на первом сообщении
	if err = client.Ping(ctx); err != nil {
		client.Close()

		return nil, fmt.Errorf("проверка соединения с kafka: %w", err)
	}

	return client, nil
}
```

`kgo` — пакет библиотеки franz-go. Один `*kgo.Client` умеет и писать, и читать. Настройки записи по умолчанию
уже надёжные: Kafka подтверждает запись, только когда сохранила её, а сетевые повторы не создают дублей на стороне брокера.

Теперь выполни `go mod tidy`.

### 7.13. Шаг 8 — relay

Relay — это бесконечный цикл: раз в секунду взять неотправленные строки, отправить, пометить.

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

	if err = r.kafka.ProduceSync(ctx, records...).FirstErr(); err != nil {
		// Транзакция откатится, строки останутся неотправленными, следующий тик повторит их.
		// Часть записей могла уже уйти — отсюда дубли (at-least-once)
		return fmt.Errorf("публикация в kafka: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = ANY($1);`, ids)
	if err != nil {
		return fmt.Errorf("пометка outbox: %w", err)
	}

	return tx.Commit(ctx)
}
```

Что происходит в `publishBatch` по шагам:

1. **Открываем транзакцию.** Она нужна, чтобы заблокировать выбранные строки, пока мы их отправляем.
2. **Берём до 100 неотправленных строк по порядку.** Про `FOR UPDATE SKIP LOCKED`:
   - `FOR UPDATE` — «эти строки мои, пока транзакция не закончится, другие их не трогают»;
   - `SKIP LOCKED` — «строки, которые уже кто-то взял, пропусти, не жди».
   Если запустить три реплики actors-service, каждая возьмёт **свои** строки, и одно событие не уйдёт трижды.
3. **Превращаем строки в `kgo.Record`** — сообщения Kafka (топик, ключ, значение).
4. **`ProduceSync`** отправляет все записи и **ждёт**, пока Kafka подтвердит каждую. `FirstErr()` — первая ошибка, если была.
5. **Ставим `published_at`** у отправленных строк.
6. **`Commit`**. Строки помечены, блокировка снята.

Если на шаге 4 или 5 что-то упало, `defer tx.Rollback` откатывает транзакцию. Строки остаются с `published_at IS NULL`,
и через секунду relay попробует снова.

### 7.14. Шаг 9 — запустить relay в actors-service

В `internal/app/actors/app.go`:

**Шаг 9.1.** После создания `pool` создай Kafka-клиент:

```go
	logger.Debug("Initializing kafka client")

	kafkaClient, err := core_kafka.NewClient(ctx, core_kafka.NewConfigMust())
	if err != nil {
		logger.Fatal("не удалось подключиться к kafka", zap.Error(err))
	}
	defer kafkaClient.Close()
```

**Шаг 9.2.** Перед запуском серверов создай relay:

```go
	relay := core_outbox.NewRelay(pool, kafkaClient, logger)
```

**Шаг 9.3.** Добавь третью горутину в `errgroup`, рядом с HTTP и gRPC:

```go
	group.Go(func() error {
		return relay.Run(groupCtx)
	})
```

Теперь actors-service — это три параллельные задачи: HTTP-сервер, gRPC-сервер и relay. При Ctrl+C все три останавливаются.

**Проверка.**

```bash
make run-actors                          # терминал 1
make kafka-tail topic=actors.events      # терминал 2: печатает всё, что приходит в топик
```

Сразу после старта relay отправит строку, оставшуюся с шага 6, и в терминале 2 появится сообщение
`10	{"event_id":...,"type":"actor.deleted",...}` (первым идёт ключ). Удали ещё одного актёра — через секунду
появится новое сообщение. В таблице `outbox` у обеих строк будет заполнен `published_at`.

**Половина пути пройдена:** actors-service надёжно публикует события. Теперь их нужно читать.

### 7.15. Шаг 10 — movies-service: удаление связей актёра

**Шаг 10.1.** Репозиторий: `internal/modules/movies/repository/postgres/remove_actor_links.go`

```go
package movie_postgres_repository

import (
	"context"
	"fmt"
)

func (r *MovieRepository) RemoveActorLinks(ctx context.Context, actorID int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `DELETE FROM movie_actors WHERE actor_id = $1;`

	if _, err := r.pool.Exec(ctx, query, actorID); err != nil {
		return fmt.Errorf("выполнение запроса: %w", err)
	}

	return nil
}
```

Обрати внимание: здесь **нет** проверки `RowsAffected() == 0 → ErrNotFound`, в отличие от `RemoveMovieActor`.
Удалено 0 строк — это нормально: у актёра могло не быть фильмов, или это повторная доставка того же события.
Ошибка здесь сломала бы идемпотентность.

Индекс `movie_actors_actor_id_dnx` по `actor_id`, который ты создал в миграции 000004, делает этот запрос быстрым.

**Шаг 10.2.** Добавь метод в интерфейс `MovieRepository` в `internal/modules/movies/service/service.go`:

```go
	RemoveActorLinks(
		ctx context.Context,
		actorID int,
	) error
```

**Шаг 10.3.** Сервис: `internal/modules/movies/service/remove_actor_links.go`

```go
package movie_service

import (
	"context"
	"fmt"
)

func (s *MovieService) RemoveActorLinks(ctx context.Context, actorID int) error {
	if err := s.movieRepository.RemoveActorLinks(ctx, actorID); err != nil {
		return fmt.Errorf("удаление связей актера/актрисы %d с фильмами: %w", actorID, err)
	}

	return nil
}
```

### 7.16. Шаг 11 — consumer

Consumer — это **ещё один транспорт** movies-service, как HTTP. HTTP-хендлер получает запрос и вызывает сервис.
Consumer получает сообщение из Kafka и вызывает сервис. Поэтому он лежит в `transport/kafka`.

`internal/modules/movies/transport/kafka/consumer.go`

```go
package movie_transport_kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	actors_events "github.com/vasya2314/golang-kp/pkg/events/actors"
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
		// Ждёт новые сообщения. Возвращает пачку (может быть из нескольких партиций)
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

		// «Закладка»: всё, что прочитали в этой пачке, обработано
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
	var envelope actors_events.Envelope
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		// Битое сообщение не починится повтором. Если ретраить — застрянем на нём навсегда
		c.logger.Error("невалидное сообщение, пропускаем", zap.Int64("offset", record.Offset), zap.Error(err))

		return nil
	}

	log := c.logger.With(
		zap.String("event_id", envelope.EventID),
		zap.String("event_type", envelope.Type),
	)
	ctx = core_logger.ToContext(ctx, log)

	switch envelope.Type {
	case actors_events.TypeActorDeleted:
		var payload actors_events.ActorDeleted
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			log.Error("невалидный payload, пропускаем", zap.Error(err))

			return nil
		}

		if err := c.movieService.RemoveActorLinks(ctx, payload.ActorID); err != nil {
			return fmt.Errorf("удаление связей актера %d: %w", payload.ActorID, err)
		}

		log.Debug("связи актера удалены", zap.Int("actor_id", payload.ActorID))

	default:
		// Незнакомый тип — не ошибка: actors-service мог добавить новое событие,
		// которое нам пока не интересно. Просто пропускаем
	}

	return nil
}
```

Как это читать:

- **`Run`** — бесконечный цикл: получить пачку сообщений → обработать каждое → закоммитить offset-ы → снова.
- **`handleWithRetry`** повторяет `handle`, пока тот не вернёт `nil`. Пауза между попытками растёт: 0.1s, 0.2s, 0.4s… до 10s.
  Если Postgres упал на минуту, consumer подождёт и продолжит, событие не потеряется.
- **`handle`** разбирает конверт, смотрит `type` и вызывает нужный метод сервиса.
- **Коммит offset-а идёт после обработки.** Если процесс упадёт посередине, после рестарта Kafka отдаст
  те же сообщения ещё раз (at-least-once). Обработчик идемпотентный, поэтому это безопасно.

Самое важное здесь — разделять два вида ошибок:

| Ошибка | Пример | Что делаем | Почему |
|---|---|---|---|
| **Временная** | Postgres недоступен, таймаут | возвращаем ошибку → повтор с паузой | через минуту БД поднимется, и всё получится |
| **Постоянная** | битый JSON | логируем и возвращаем `nil` → идём дальше | повтор никогда не поможет. Если повторять, consumer застрянет (poison pill), а за этим сообщением встанет вся партиция |

**`ctx = core_logger.ToContext(ctx, log)`** — то же, что делают HTTP-middleware и gRPC-interceptor: кладём логгер
в контекст, иначе `core_logger.FromContext` в сервисном слое упадёт.

### 7.17. Шаг 12 — запустить consumer в movies-service

Сейчас `app_movies.Run` запускает только HTTP-сервер через `server.Run(ctx)`. Теперь задач две,
поэтому переходим на `errgroup`, как в actors-service.

В `internal/app/movies/app.go`:

**Шаг 12.1.** После создания `movieService` создай Kafka-клиент с настройками consumer-а и сам consumer:

```go
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
```

**Шаг 12.2.** Замени запуск сервера в конце функции:

```go
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
```

**Откуда consumer начнёт читать.** Группа `movies-service` новая, закладки у неё ещё нет. В этом случае franz-go
по умолчанию читает топик **с самого начала**, поэтому события, отправленные на шаге 9, тоже будут обработаны.
При следующих запусках чтение продолжится с закладки.

**Проверка.** Запусти оба сервиса (`make app-run`). В логе movies-service должны появиться строки
«связи актера удалены» для событий, которые уже лежат в топике.

Теперь попробуй полный путь:
1. Создай актёра и привяжи его к фильму (`PUT localhost:5055/api/v1/movies/<movieId>/actors/<actorId>`).
2. Удали актёра (`DELETE localhost:5056/api/v1/actors/<actorId>`).
3. `GET localhost:5055/api/v1/movies/<movieId>/actors` — актёра в списке нет.

Но пока это ещё ничего не доказывает: связи по-прежнему удаляет и FK с каскадом. Убираем его.

### 7.18. Шаг 13 — миграция: убрать внешний ключ на actors

**Шаг 13.1.** Посмотри точное имя ограничения:

```bash
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp -c '\d golang_kp.movie_actors'
```

В разделе `Foreign-key constraints` будет что-то вроде `movie_actors_actor_id_fkey`.
Postgres по умолчанию называет так: `<таблица>_<колонка>_fkey`.

**Шаг 13.2.**

```bash
make migrate-create seq=drop_movie_actors_actor_fk
```

`000007_drop_movie_actors_actor_fk.up.sql`:

```sql
ALTER TABLE golang_kp.movie_actors DROP CONSTRAINT movie_actors_actor_id_fkey;
```

`000007_drop_movie_actors_actor_fk.down.sql` — вернуть как было:

```sql
ALTER TABLE golang_kp.movie_actors
    ADD CONSTRAINT movie_actors_actor_id_fkey
    FOREIGN KEY (actor_id) REFERENCES golang_kp.actors (id) ON DELETE CASCADE;
```

**Шаг 13.3.** `make migrate-up`

Внешний ключ на `movies` **оставляем**: `movie_actors` и `movies` — обе таблицы movies-service, они останутся в одной базе.

**Почему этот шаг последний.** Если убрать FK раньше, чем заработает consumer, то каждое удаление актёра
в промежутке оставляло бы мусорные связи. Сейчас замена уже работает, и старый механизм можно выключать.

### 7.19. Почему BatchGetActors может вернуть меньше актёров, чем было id

Это ответ на вопрос из 6.7. Представь, что два действия случились почти одновременно:

```
t1  movies:   AddMovieActor → gRPC GetActor(10) → «актёр есть»
t2  actors:   DELETE актёра 10 + запись в outbox
t3  movies:   INSERT INTO movie_actors (1, 10)          ← актёра уже нет!
t4  movies:   consumer получил actor.deleted(10) → DELETE связей   ← мусор убран
```

Между t2 и t4 в `movie_actors` лежит id актёра, которого уже нет. Обычно это доли секунды,
но если Kafka лежит, то может быть и час. Если в этот момент кто-то запросит `GET /movies/1/actors`,
`BatchGetActors` просто не найдёт актёра 10 и вернёт остальных.

Это и есть **eventual consistency**: данные двух сервисов какое-то время расходятся, но в итоге сходятся.
Заметь, что гонку t1→t3 закрывает то же самое событие: DELETE в consumer выполнится после INSERT.
Поэтому `GetMovieActors` возвращает только найденных актёров и не считает это ошибкой.

**В этом и есть цена разделения:** раньше FK гарантировал целостность всегда и мгновенно, теперь — только «в итоге».

### 7.20. Шаг 14 — проверка Kafka-части

Запусти всё в трёх терминалах:

```bash
make run-actors                          # терминал 1
make run-movies                          # терминал 2
make kafka-tail topic=actors.events      # терминал 3
```

| # | Что делаешь | Что должно получиться | Что это доказывает |
|---|---|---|---|
| 1 | Привяжи актёра к фильму, удали актёра | в терминале 3 — событие; в логе movies — «связи актера удалены»; у фильма актёра нет | вся цепочка работает |
| 2 | Останови movies-service (Ctrl+C), удали другого актёра с фильмами, запусти movies снова | после старта связи удалились | Kafka хранит события, пока получатель выключен |
| 3 | `docker compose stop golang-kp-kafka`, удали актёра | ответ **204**; в `outbox` строка с пустым `published_at`; в логе actors — ошибки relay раз в секунду | удаление не зависит от Kafka |
| 4 | `docker compose start golang-kp-kafka` | через пару секунд событие в топике, `published_at` заполнен, связи удалены | relay доотправил накопленное |
| 5 | Ctrl+C у movies-service | в логах «Shutting down», без паник | errgroup корректно гасит HTTP и consumer |

Сценарии 2–4 — самое ценное в этом разделе. Обязательно прогони их руками.

**Самопроверка:**
1. Почему relay держит транзакцию открытой, пока ждёт ответа Kafka? Что было бы, если сначала поставить
   `published_at`, закоммитить, а потом отправлять?
2. Таблица `outbox` растёт бесконечно. Как бы ты её чистил? (Подсказка: фоновое удаление строк,
   у которых `published_at` старше N дней.)
3. Почему `RemoveActorLinks` не возвращает `ErrNotFound`, когда удалено 0 строк?
4. Что случится, если в `handle` для битого JSON вернуть ошибку вместо `nil`?
5. Запустили две реплики movies-service. Сколько раз удалятся связи по одному событию? А если запустить search-service
   со своей группой?

---

## 8. Шаг 4 — раздельные базы данных

### 8.1. Что мы делаем

К этому моменту movies-service уже **не читает и не пишет** таблицы `actors` и `outbox`: актёров получает по gRPC,
об удалениях узнаёт из Kafka. Связь между сервисами осталась только физическая: таблицы лежат в одной базе.
Теперь переносим таблицы actors-service в отдельную базу `golang_kp_actors`.

```
Было:  Postgres ─ база golang_kp ─ movies, movie_actors, actors, outbox   ← обоим сервисам
Стало: Postgres ─ база golang_kp        ─ movies, movie_actors            ← только movies-service
              └─ база golang_kp_actors ─ actors, outbox                   ← только actors-service
```

Сервер Postgres остаётся один (контейнер `golang-kp-postgres`), баз в нём становится две.
Для изоляции важна именно **отдельная база**, к которой другой сервис не подключается.
На одном сервере она или на разных — вопрос нагрузки и денег. В проде часто один кластер Postgres и много баз.

| # | Что делаем | Как проверить |
|---|---|---|
| 1 | Разложить миграции по папкам сервисов | — |
| 2 | Научить `Makefile` работать с папкой и базой конкретного сервиса | `make migrate-action svc=movies action=version` → 7 |
| 3 | Миграции для базы actors | — |
| 4 | Создать базу и накатить миграции | `\dt golang_kp.*` в новой базе |
| 5 | Перенести данные | количество актёров совпадает |
| 6 | Переключить actors-service на новую базу | `GET /actors` работает |
| 7 | Удалить `actors` и `outbox` из базы movies | всё работает, как раньше |

### 8.2. Шаг 1 — миграции по папкам

```bash
mkdir -p migrations/movies migrations/actors
git mv migrations/0000*.sql migrations/movies/
```

Все существующие миграции (000001–000007) относятся к базе movies: именно в ней они уже накатаны.
golang-migrate хранит номер последней накатанной миграции в таблице `schema_migrations` **внутри базы**,
поэтому перенос файлов в другую папку ничего не ломает: номер в базе остаётся 7.

### 8.3. Шаг 2 — Makefile с параметром svc

Теперь каждая команда миграций должна знать **какой сервис**: от этого зависят папка и база.
Передаём это параметром: `make migrate-up svc=actors`.

Вверху `Makefile`, после `export`, добавь имена баз:

```make
DB_movies := ${POSTGRES_DB}
DB_actors := golang_kp_actors
```

Приём `$(DB_$(svc))` — «переменная в имени переменной»: при `svc=actors` он превращается в `$(DB_actors)`,
то есть `golang_kp_actors`.

Замени цели миграций целиком:

```make
check-svc:
	@if [ "$(svc)" != "movies" ] && [ "$(svc)" != "actors" ]; then \
	    echo "Укажи сервис: svc=movies или svc=actors"; \
	    exit 1; \
	fi

db-create-actors:
	@docker compose exec -T golang-kp-postgres \
		psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -c "CREATE DATABASE ${DB_actors};"

migrate-create: check-svc
	@if [ -z "$(seq)" ]; then \
	    echo "Отсутствует обязательный параметр seq. Пример: make migrate-create svc=movies seq=init"; \
	    exit 1; \
	fi; \
	docker compose run --rm golang-kp-migrate \
		create \
		-ext sql \
		-dir /migrations/$(svc) \
		-seq "$(seq)"

migrate-up:
	@make migrate-action svc=$(svc) action=up n=$(n)

migrate-down:
	@make migrate-action svc=$(svc) action=down n=$(if $(n),$(n),1)

migrate-ensure-schema: check-svc
	@docker compose exec -T golang-kp-postgres \
		psql -U ${POSTGRES_USER} -d $(DB_$(svc)) -v ON_ERROR_STOP=1 \
		-c "CREATE SCHEMA IF NOT EXISTS golang_kp;" > /dev/null

migrate-action: check-svc
	@if [ -z "$(action)" ]; then \
	    echo "Отсутствует обязательный параметр action."; \
		exit 1; \
	fi; \
	make migrate-ensure-schema svc=$(svc) && \
	docker compose run --rm golang-kp-migrate \
		-path /migrations/$(svc) \
		-database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@golang-kp-postgres:5432/$(DB_$(svc))?sslmode=disable&search_path=golang_kp" \
		$(action) $(n)
```

Что изменилось по сравнению со старой версией:
- `check-svc` не даёт запустить миграцию, забыв указать сервис;
- `-dir /migrations/$(svc)` и `-path /migrations/$(svc)` — папка сервиса. В `docker-compose.yaml` монтируется вся папка
  `migrations`, поэтому подпапки видны внутри контейнера без правок;
- `$(DB_$(svc))` вместо `${POSTGRES_DB}` — база сервиса.

**Проверка:**

```bash
make migrate-action svc=movies action=version
```

Должно напечатать `7`. Значит, Makefile смотрит в правильную папку и базу.

### 8.4. Шаг 3 — миграции для базы actors

В новой базе нужны таблицы `actors` и `outbox`. SQL для них у тебя уже есть, его можно скопировать:

```bash
cp migrations/movies/000003_add_actor_entity.up.sql   migrations/actors/000001_add_actor_entity.up.sql
cp migrations/movies/000003_add_actor_entity.down.sql migrations/actors/000001_add_actor_entity.down.sql
cp migrations/movies/000006_add_outbox.up.sql         migrations/actors/000002_add_outbox.up.sql
cp migrations/movies/000006_add_outbox.down.sql       migrations/actors/000002_add_outbox.down.sql
```

Нумерация в `migrations/actors` своя и начинается с 000001: у каждой базы своя история миграций.

### 8.5. Шаг 4 — создать базу и накатить миграции

```bash
make db-create-actors
make migrate-up svc=actors
```

Проверка:

```bash
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp_actors -c '\dt golang_kp.*'
```

Должны быть таблицы `actors`, `outbox` и служебная `schema_migrations`. Пока пустые.

### 8.6. Шаг 5 — перенести данные

**Шаг 5.1. Останови actors-service.** Пока он работает, кто-нибудь может создать актёра в старой базе, и он не переедет.

**Шаг 5.2. Убедись, что relay всё отправил:**

```bash
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp \
  -c 'SELECT count(*) FROM golang_kp.outbox WHERE published_at IS NULL;'
```

Должно быть `0`. Таблицу `outbox` мы не переносим: отправленные события больше не нужны.
Если там что-то осталось, запусти actors-service, подожди пару секунд и снова останови.

**Шаг 5.3. Скопируй актёров** из старой базы в новую:

```bash
docker compose exec -T golang-kp-postgres \
  pg_dump -U golang_kp -d golang_kp --data-only -t golang_kp.actors \
| docker compose exec -T golang-kp-postgres \
  psql -U golang_kp -d golang_kp_actors
```

`pg_dump --data-only -t golang_kp.actors` выгружает **только данные** одной таблицы в виде SQL.
Вертикальная черта `|` передаёт этот SQL в `psql`, подключённый к новой базе. Структура таблицы там уже есть (шаг 4).

**Шаг 5.4. Сдвинь счётчик id.** У колонки `id SERIAL` есть счётчик (sequence), который выдаёт следующий id.
В новой базе он начинается с 1, и следующий `INSERT` упадёт: актёр с `id = 1` уже есть.

```bash
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp_actors \
  -c "SELECT setval('golang_kp.actors_id_seq', (SELECT max(id) FROM golang_kp.actors));"
```

**Шаг 5.5. Сверь количество** в старой и новой базе:

```bash
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp        -c 'SELECT count(*) FROM golang_kp.actors;'
docker compose exec golang-kp-postgres psql -U golang_kp -d golang_kp_actors -c 'SELECT count(*) FROM golang_kp.actors;'
```

### 8.7. Шаг 6 — переключить actors-service на новую базу

В `Makefile`, в цели `run-actors`, добавь базу:

```make
run-actors:
	@export LOGGER_FOLDER=${PROJECT_ROOT}/out/logs/actors && \
	export POSTGRES_HOST=localhost && \
	export POSTGRES_DB=${DB_actors} && \
	export HTTP_ADDRESS=:5056 && \
	go run ${PROJECT_ROOT}/cmd/actors-service
```

Код менять не нужно: `core_pgx_pool` и так читает имя базы из `POSTGRES_DB`.

Запусти `make run-actors` и проверь: `curl localhost:5056/api/v1/actors` возвращает тех же актёров;
создание нового актёра получает следующий id, а не 1.

### 8.8. Шаг 7 — убрать actors и outbox из базы movies

Делаем это **последним**, когда уже проверили, что actors-service работает с новой базой. До этого момента
старые таблицы — резервная копия: если что-то пошло не так, можно вернуть `POSTGRES_DB` и откатиться.

```bash
make migrate-create svc=movies seq=drop_actors_and_outbox
```

`000008_drop_actors_and_outbox.up.sql`:

```sql
DROP TABLE golang_kp.outbox;
DROP TABLE golang_kp.actors;
```

`000008_drop_actors_and_outbox.down.sql` — восстановить структуру (данные, конечно, не вернутся).
Скопируй сюда содержимое `000003_add_actor_entity.up.sql` и `000006_add_outbox.up.sql`.

```bash
make migrate-up svc=movies
```

`DROP TABLE actors` пройдёт только потому, что на шаге 13 раздела 7 ты убрал внешний ключ `movie_actors → actors`.
Иначе Postgres не дал бы удалить таблицу, на которую кто-то ссылается.

**Проверка:** запусти оба сервиса и прогони сценарии из раздела 9.

**Как это делают в проде.** Там нельзя остановить сервис даже на минуту, поэтому переезд растягивают на несколько шагов:
пишут в старую и новую базу одновременно, копируют старые данные в фоне, сверяют, переключают чтение на новую
и только потом выключают старую. Или используют CDC (Debezium). Для учебного проекта остановки на минуту достаточно.

---

## 9. Проверка всего вместе

```bash
make env-up
make kafka-create-topics                 # если топика ещё нет
make run-actors                          # терминал 1
make run-movies                          # терминал 2
make kafka-tail topic=actors.events      # терминал 3
```

Каждый сценарий показывает, **зачем** нужна соответствующая часть системы:

| # | Что делаешь | Что должно получиться | Что это доказывает |
|---|---|---|---|
| 1 | `PUT localhost:5055/api/v1/movies/1/actors/999` (такого актёра нет) | 404 | gRPC `GetActor` вместо FK |
| 2 | `GET localhost:5055/api/v1/movies/1/actors` | актёры с именами и датами | gRPC `BatchGetActors` вместо JOIN |
| 3 | `DELETE localhost:5056/api/v1/actors/<id>` | событие в терминале 3, связи у фильмов исчезли | Kafka вместо `ON DELETE CASCADE` |
| 4 | Останови actors-service, повтори сценарий 2 | **503**, movies-service при этом жив | синхронная зависимость: ретраи, `FromStatus` |
| 5 | Останови **movies-service**, удали актёра, запусти movies | связи удалятся после старта | Kafka хранит события, пока получатель выключен |
| 6 | `docker compose stop golang-kp-kafka`, удали актёра | **204**, в `outbox` строка с пустым `published_at` | outbox: удаление не зависит от Kafka |
| 7 | `docker compose start golang-kp-kafka` | через пару секунд событие в топике, связи удалены | relay доотправил накопленное |
| 8 | Ctrl+C у обоих сервисов | «Shutting down» в логах, без паник | graceful shutdown через errgroup |

Сценарии 4–7 — самое ценное. Обязательно прогони их руками.

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
| FK | gRPC `GetActor` | сетевой вызов, 503 при недоступности, гонка (7.19) |
| JOIN | gRPC `BatchGetActors` | второй запрос, обязательный batch |
| `ON DELETE CASCADE` | outbox → Kafka → consumer | таблица outbox, relay, идемпотентность, eventual consistency |
| одна транзакция | транзакция на сервис + события | at-least-once, дубли, порядок только внутри партиции |
| один процесс | два процесса + Kafka | больше инфраструктуры, логи в двух местах, трейсинг |

Если на собеседовании спросят «почему вы распилили монолит», правильный ответ начинается с проблемы,
которую это решило, а не с технологий. Если сможешь объяснить эту таблицу, значит, разобрался.
