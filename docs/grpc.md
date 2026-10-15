# gRPC в golang-kp: как это устроено

Это не инструкция «как сделать» (она в `microservices.md`, раздел 6), а **карта уже написанного кода**:
какой файл за что отвечает, как запрос проходит от HTTP-клиента до базы actors-service и обратно,
где решаются таймауты и ошибки.

Читать лучше по порядку: сначала общая картина, потом контракт, сервер, клиент, сквозной пример.
В конце — что в текущей реализации можно улучшить и вопросы для самопроверки.

---

## 1. Общая картина

```
                    HTTP :5055                                   gRPC :9090
  пользователь ───────────────────▶  movies-service  ───────────────────────▶  actors-service
                                     │  (gRPC-КЛИЕНТ)                           │  (gRPC-СЕРВЕР
                                     │                                          │   + HTTP :5056)
                                     ▼                                          ▼
                                 Postgres: movies,                          Postgres: actors
                                 movie_actors (только id)
```

- **actors-service** — владелец данных об актёрах. Он поднимает **два** сервера одновременно:
  HTTP (для внешних клиентов, CRUD актёров) и gRPC (для других сервисов).
- **movies-service** — хранит фильмы и связи `movie_id ↔ actor_id`. Когда ему нужны данные актёра
  (имя, дата рождения) или проверка «такой актёр существует?», он идёт в actors-service по gRPC.
- gRPC-API **внутренний**: наружу пользователям он не публикуется, только сервис → сервис.

Сейчас у сервера два метода:

| Метод | Кто вызывает | Зачем |
|---|---|---|
| `GetActor` | `movie_service.AddMovieActor` | проверить, что актёр существует, перед привязкой к фильму (замена FOREIGN KEY) |
| `BatchGetActors` | `movie_service.GetMovieActors` | получить данные всех актёров фильма **одним** запросом (замена JOIN, без N+1) |

---

## 2. Карта файлов

### Контракт и генерация

| Файл | Что это |
|---|---|
| `api/proto/actors/v1/actors.proto` | **Контракт.** Единственный источник правды: какие методы и какие сообщения. Пишется руками |
| `buf.yaml` | Настройки `buf`: где лежат proto (`api/proto`), правила линтера и проверки breaking changes |
| `buf.gen.yaml` | Как генерировать Go-код: какие плагины, куда класть, какой `go_package` |
| `pkg/api/actors/v1/actors.pb.go` | **Сгенерировано.** Go-структуры сообщений (`Actor`, `GetActorRequest`…) и их сериализация |
| `pkg/api/actors/v1/actors_grpc.pb.go` | **Сгенерировано.** Интерфейсы сервера и клиента, `RegisterActorServiceServer`, `NewActorServiceClient` |
| `Makefile` → `proto-gen` | `buf lint && buf generate` |

### Общая инфраструктура (`internal/core`) — не знает про актёров

| Файл | Что это |
|---|---|
| `internal/core/transport/grpc/server/server.go` | Обёртка над `grpc.Server`: interceptors, reflection, запуск и graceful shutdown |
| `internal/core/transport/grpc/server/config.go` | Конфиг сервера из env с префиксом `GRPC_` |
| `internal/core/transport/grpc/server/errors.go` | Перевод ошибок: `ToStatus` (наша ошибка → gRPC-код, сервер) и `FromStatus` (gRPC-код → наша ошибка, клиент) |
| `internal/core/transport/grpc/interceptor/interceptor.go` | Серверные interceptors: логгер в контексте + логирование, recovery от паник |

### Сервер (actors-service)

| Файл | Что это |
|---|---|
| `internal/modules/actors/transport/grpc/server.go` | **Адаптер** gRPC → сервисный слой. Реализует сгенерированный интерфейс `ActorServiceServer` |
| `internal/modules/actors/service/get_actors_by_ids.go` | Бизнес-логика для batch-запроса (тот же сервис, что и для HTTP) |
| `internal/modules/actors/repository/postgres/get_actors_by_ids.go` | SQL `WHERE id = ANY($1)` |
| `internal/app/actors/app.go` | Сборка: создаёт gRPC-сервер, регистрирует адаптер, запускает рядом с HTTP через `errgroup` |
| `cmd/actors-service/main.go` | Точка входа, просто `app_actors.Run()` |

### Клиент (movies-service)

| Файл | Что это |
|---|---|
| `internal/modules/movies/client/actors/client.go` | **gRPC-клиент**: соединение, service config (таймаут + ретраи), методы, перевод proto → domain |
| `internal/modules/movies/client/actors/config.go` | Конфиг клиента из env с префиксом `ACTORS_GRPC_` |
| `internal/modules/movies/service/service.go` | Интерфейс `ActorProvider` — то, что сервису фильмов нужно от «кого-то, кто знает актёров» |
| `internal/modules/movies/service/add_movie_actor.go` | Вызывает `GetActor` |
| `internal/modules/movies/service/get_movie_actors.go` | Вызывает `GetActorsByIDs` |
| `internal/modules/movies/repository/postgres/get_movie_actor_ids.go` | Достаёт из своей БД только id актёров фильма |
| `internal/app/movies/app.go` | Сборка: создаёт клиент, передаёт его в `NewMovieService` как `ActorProvider` |

### Окружение

| Где | Переменная | Значение | Кто читает |
|---|---|---|---|
| `.env` | `GRPC_ADDRESS` | `:9090` | actors-service: на каком порту слушать |
| `.env` | `ACTORS_GRPC_ADDRESS` | `localhost:9090` | movies-service: куда подключаться |
| — | `GRPC_SHUTDOWN_TIMEOUT` | `30s` по умолчанию | сколько ждать graceful stop |

Обрати внимание на разницу: сервер слушает `:9090` (все интерфейсы), клиент ходит на `host:port`.
В docker-compose/Kubernetes `localhost` заменится на имя сервиса (`actors-service:9090`).

### Слои — одной картинкой

```
movies-service                                         actors-service
──────────────                                         ──────────────
transport/http  (GetMovieActors handler)
      │
service         (MovieService)   ── ActorProvider ──┐
      │                              (интерфейс)    │
repository      (movie ids)                         │
                                                    ▼
                               client/actors (ActorsClient)
                                     │ actorsv1.ActorServiceClient (сгенерирован)
                                     │
                    ═════════════ сеть, HTTP/2, protobuf ═════════════
                                     │
                                     ▼
                               grpc.Server (core)  → interceptors
                                     │
                               transport/grpc (ActorGRPCServer)
                                     │
                               service (ActorService)  ← тот же, что у HTTP
                                     │
                               repository (Postgres)
```

Главная мысль: **gRPC живёт только в двух местах** — в `transport/grpc` у сервера и в `client/actors` у клиента.
Сервисный слой обоих сервисов про gRPC не знает. Сервер — это ещё один транспорт рядом с HTTP,
а клиент — ещё одна реализация зависимости, как репозиторий или S3.

---

## 3. Контракт: `actors.proto`

```proto
package actors.v1;

service ActorService {
  rpc GetActor(GetActorRequest) returns (GetActorResponse);
  rpc BatchGetActors(BatchGetActorsRequest) returns (BatchGetActorsResponse);
}
```

На что смотреть:

- **`package actors.v1`** — версия в имени пакета. Если когда-нибудь понадобится несовместимое изменение,
  рядом появится `actors.v2`, а старые клиенты продолжат работать с `v1`. Это требование линтера `STANDARD` в buf.
- **Полное имя метода** = `/<package>.<Service>/<Method>`, например `/actors.v1.ActorService/GetActor`.
  Оно видно в логах (`grpc_method`), в grpcurl и в service config клиента (`"service": "actors.v1.ActorService"`).
- **Request/Response на каждый метод**, даже если внутри одно поле. Так можно добавлять поля, не ломая сигнатуру.
- **Номера полей** (`= 1`, `= 2`…) — это то, что реально летит по сети, а не имена.
  Номер нельзя менять или переиспользовать; переименовать поле можно.
- **`optional string middle_name`** — в proto3 скалярные поля без `optional` не различают «не задано» и «пустая строка».
  С `optional` в Go получается `*string`, ровно как `MiddleName *string` в домене.
- **`birth_date` как строка `YYYY-MM-DD`** — простое решение; альтернатива — `google.type.Date`
  или `google.protobuf.Timestamp` (для дня рождения timestamp не подходит из-за часовых поясов).
- **`BatchGetActorsResponse` возвращает только найденных.** Отсутствующий id — не ошибка.
  Это стандартная семантика batch-get (так устроены API Google).

### Генерация

`make proto-gen` → `buf lint && buf generate`. В `buf.gen.yaml` два плагина:

| Плагин | Что генерирует |
|---|---|
| `buf.build/protocolbuffers/go` | `actors.pb.go` — структуры сообщений |
| `buf.build/grpc/go` | `actors_grpc.pb.go` — клиент и сервер |

Плагины `remote` — выполняются на серверах buf, локально `protoc-gen-go` ставить не нужно.
`managed.go_package_prefix` избавляет от строки `option go_package = ...` в каждом .proto.

**Сгенерированный код не редактируют руками** — он перезапишется при следующей генерации.
Его коммитят в репозиторий (так делает большинство проектов: `go build` не должен требовать buf).

### Что внутри `actors_grpc.pb.go` (полезно один раз открыть)

```go
// Константы полных имён методов
ActorService_GetActor_FullMethodName = "/actors.v1.ActorService/GetActor"

// КЛИЕНТ: интерфейс + реализация. Метод — это просто cc.Invoke с именем метода
type ActorServiceClient interface { GetActor(ctx, in, opts...) (*GetActorResponse, error); ... }
func NewActorServiceClient(cc grpc.ClientConnInterface) ActorServiceClient

// СЕРВЕР: интерфейс, который мы реализуем
type ActorServiceServer interface {
    GetActor(context.Context, *GetActorRequest) (*GetActorResponse, error)
    BatchGetActors(...)
    mustEmbedUnimplementedActorServiceServer()   // ← неэкспортируемый метод
}
type UnimplementedActorServiceServer struct{}   // заглушки: возвращают codes.Unimplemented

// Регистрация реализации на сервере
func RegisterActorServiceServer(s grpc.ServiceRegistrar, srv ActorServiceServer)
var ActorService_ServiceDesc = grpc.ServiceDesc{...}  // таблица «имя метода → хендлер»
```

Из-за `mustEmbedUnimplementedActorServiceServer()` интерфейс **нельзя** реализовать, не встроив
`UnimplementedActorServiceServer`. Это намеренно: если в proto добавят метод, старый сервер всё равно
скомпилируется и для нового метода вернёт `Unimplemented`, а не сломает сборку.
`testEmbeddedByValue` в `Register...` проверяет, что встроили по значению, а не указателем (nil-указатель упал бы с паникой).

---

## 4. Сервер (actors-service)

### 4.1. `core/transport/grpc/server/server.go` — сам сервер

```go
server := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        core_grpc_interceptor.LoggerInterceptor(logger),
        core_grpc_interceptor.RecoveryInterceptor(),
    ),
)
reflection.Register(server)
```

**Interceptor** — это middleware для gRPC. Unary = «один запрос → один ответ» (не стрим).
Цепочка работает как матрёшка, первый — самый внешний:

```
запрос → LoggerInterceptor → RecoveryInterceptor → handler (ActorGRPCServer.GetActor)
ответ  ← LoggerInterceptor ← RecoveryInterceptor ← handler
```

- **LoggerInterceptor** кладёт логгер в `ctx` с полем `grpc_method`. Без этого `core_logger.FromContext(ctx)`
  в сервисном слое упадёт — в HTTP то же самое делает middleware `Logger`. Потом пишет код ответа и длительность.
- **RecoveryInterceptor** ловит панику в хендлере и превращает её в `codes.Internal`, иначе один баг уронит весь процесс.
  Он стоит **внутри** логгера, поэтому в момент паники логгер уже в контексте.

**Reflection** — сервер сам рассказывает, какие у него сервисы и методы. Нужно для `grpcurl` без .proto-файла.
В проде его часто выключают или закрывают, чтобы не раскрывать внутренний API.

`Server` встраивает `*grpc.Server`, поэтому у него есть все методы оригинала —
в том числе `RegisterService`, и его можно передать в `RegisterActorServiceServer`.

### 4.2. `Run` — запуск и graceful shutdown

```
net.Listen(tcp, :9090)
go Serve(listener)          ── блокируется, пока сервер работает
select:
  ├─ Serve вернул ошибку     → вернуть ошибку
  └─ ctx отменён (SIGTERM)   → GracefulStop в горутине
                                ├─ успел за ShutdownTimeout → ок
                                └─ не успел                  → Stop (рвём соединения)
```

- `GracefulStop` перестаёт принимать новые запросы и **ждёт** завершения текущих. Он может ждать вечно
  (например, долгий стрим), поэтому он в горутине и ограничен `time.After`.
- `Stop` закрывает всё сразу. Запущенные запросы получат ошибку.

Логика та же, что у HTTP-сервера, только у `http.Server.Shutdown` таймаут передаётся через контекст,
а у gRPC его приходится делать вручную.

### 4.3. `actors/transport/grpc/server.go` — адаптер

Это то же самое, что HTTP-хендлеры в `transport/http`, только вход и выход — protobuf, а не JSON.
Каждый метод делает три вещи:

1. **proto → домен:** `int(req.GetId())`, `[]int64 → []int`.
2. **Вызов сервиса:** `s.actorService.GetActor(ctx, id)` — того же `ActorService`, что использует HTTP.
3. **домен → proto** (`actorToProto`) или **ошибка → gRPC-статус** (`core_grpc_server.ToStatus`).

Детали:

- Адаптер зависит не от `*actor_service.ActorService`, а от **своего** маленького интерфейса `ActorService`
  с двумя методами — ровно тем, что ему нужно (принцип «интерфейс объявляет потребитель»).
- **Геттеры** (`req.GetId()`) вместо полей (`req.Id`): геттер безопасен на nil-сообщении.
- **`maxBatchSize = 100`** — защита сервера: без лимита один запрос с миллионом id положит базу.
  Превышение → `InvalidArgument`.

### 4.4. `core/transport/grpc/server/errors.go` — ошибки, сторона сервера

В gRPC ошибка — это не HTTP-статус, а **`status`**: код (`codes.NotFound`, …) + сообщение.
`ToStatus` переводит наши sentinel-ошибки:

| Наша ошибка | gRPC-код | Сообщение |
|---|---|---|
| `ErrNotFound` | `NotFound` | текст ошибки |
| `ErrInvalidArgument` | `InvalidArgument` | текст ошибки |
| `ErrConflict` | `Aborted` | текст ошибки |
| всё остальное | `Internal` | `"внутренняя ошибка"` — детали наружу не отдаём |

Если вернуть из хендлера обычный `error`, а не `status`, gRPC сам превратит его в `codes.Unknown` —
клиент не сможет понять, что случилось. Поэтому каждая ошибка проходит через `ToStatus`.

### 4.5. `internal/app/actors/app.go` — сборка

```go
actorService := actor_service.NewActorService(actorRepository)        // ОДИН сервис…
actorTransportHTTP := actor_transport_http.NewActorHTTPHandler(actorService)   // …для HTTP
grpcServer := core_grpc_server.NewServer(core_grpc_server.MustLoadConfig(), logger)
actorsv1.RegisterActorServiceServer(grpcServer, actor_transport_grpc.NewActorGRPCServer(actorService)) // …и для gRPC

group, groupCtx := errgroup.WithContext(ctx)
group.Go(func() error { return server.Run(groupCtx) })
group.Go(func() error { return grpcServer.Run(groupCtx) })
group.Wait()
```

**`errgroup`** запускает оба сервера параллельно и связывает их судьбы:
- если один упал с ошибкой (например, порт занят), `groupCtx` отменяется и второй тоже корректно останавливается;
- при SIGTERM отменяется родительский `ctx` → `groupCtx` → оба делают graceful shutdown;
- `Wait` возвращает первую ошибку.

---

## 5. Клиент (movies-service)

### 5.1. `client/actors/client.go` — создание соединения

```go
conn, err := grpc.NewClient(
    config.Address,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithDefaultServiceConfig(serviceConfig),
)
client := actorsv1.NewActorServiceClient(conn)
```

- **`grpc.NewClient` не подключается.** Он только создаёт объект `ClientConn`. TCP-соединение откроется
  при первом вызове. Поэтому movies-service стартует, даже если actors-service ещё не запущен,
  а ошибка придёт на первом запросе (`Unavailable`).
- **Один `ClientConn` на всё приложение.** Он потокобезопасен, внутри — HTTP/2 соединение, по которому
  параллельно идут все запросы (мультиплексирование). Создавать клиент на каждый запрос — классическая ошибка.
- **`insecure`** — без TLS. Нормально внутри docker-сети или кластера с mesh (mTLS сделает sidecar);
  в остальных случаях — `credentials.NewTLS(...)`.
- **`Close()`** вызывается через `defer actorsClient.Close()` в `app_movies.Run`.

### 5.2. Service config — таймаут и ретраи

```json
{
  "methodConfig": [{
    "name": [{"service": "actors.v1.ActorService"}],
    "timeout": "2s",
    "retryPolicy": {
      "maxAttempts": 3,
      "initialBackoff": "0.1s", "maxBackoff": "1s", "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}
```

- `name: [{service: ...}]` без `method` — правило для **всех** методов сервиса.
- `timeout: 2s` — дедлайн на **весь** вызов, включая ретраи. Если в `ctx` уже есть более ранний дедлайн, побеждает он.
- `retryPolicy` — до 3 попыток (1 + 2 повтора) с паузами ~0.1s, ~0.2s (с jitter), только на `UNAVAILABLE`
  («до сервера не достучались»). Ретраить безопасно, потому что оба метода только читают.
  `INTERNAL` или `DEADLINE_EXCEEDED` не ретраятся: сервер мог уже что-то сделать или просто тормозит,
  и повтор только добавит нагрузки.

JSON-строка — официальный формат grpc-go (общий для всех языков gRPC). Альтернативы —
retry-interceptor из `go-grpc-middleware` или сборка JSON из Go-структуры — в разделе 9.

### 5.3. Методы клиента

```go
func (c *ActorsClient) GetActor(ctx context.Context, id int) (domain.Actor, error) {
    resp, err := c.client.GetActor(ctx, &actorsv1.GetActorRequest{Id: int64(id)})
    if err != nil {
        return domain.Actor{}, fmt.Errorf("actors.GetActor: %w", core_grpc_server.FromStatus(err))
    }
    return actorFromProto(resp.GetActor())
}
```

Зеркало серверного адаптера: домен → proto, вызов, proto → домен (`actorFromProto`), статус → наша ошибка.
`actorFromProto` ещё и проверяет ответ: nil-актёр и непарсящаяся дата — это ошибка, а не «пустой актёр».
Данные из сети — недоверенные, даже если их прислал наш же сервис.

### 5.4. `FromStatus` — ошибки, сторона клиента

| gRPC-код | Наша ошибка | HTTP-ответ пользователю movies-service |
|---|---|---|
| `NotFound` | `ErrNotFound` | 404 |
| `InvalidArgument` | `ErrInvalidArgument` | 400 |
| `Unavailable`, `DeadlineExceeded` | `ErrServiceUnavailable` | 503 |
| всё остальное | обёрнутая исходная ошибка | 500 |

503 для `ErrServiceUnavailable` добавлен в `core/transport/http/response/handler.go`.

### 5.5. `ActorProvider` — почему сервис фильмов не знает про gRPC

```go
// internal/modules/movies/service/service.go
type ActorProvider interface {
    GetActor(ctx context.Context, id int) (domain.Actor, error)
    GetActorsByIDs(ctx context.Context, ids []int) ([]domain.Actor, error)
}
```

`MovieService` зависит от интерфейса, `*ActorsClient` ему удовлетворяет (Go-интерфейсы реализуются неявно).
В `app_movies.Run` клиент передаётся в `NewMovieService(..., actorsClient)`.

Что это даёт:
- в тестах сервиса подставляется фейк без сети;
- если завтра актёров начнут получать по HTTP или из кеша — меняется только `client/`, сервис не трогаем;
- в сервисном слое нет ни `actorsv1`, ни `codes.*` — только домен и наши ошибки.

---

## 6. Сквозной пример: `GET /api/v1/movies/7/actors`

```
movies-service                                                     actors-service
──────────────                                                     ──────────────
1. chi → GetMovieActors (transport/http)
2. MovieService.GetMovieActors
   ├─ movieRepository.GetMovie(7)        → нет фильма? 404, в сеть не идём
   ├─ movieRepository.GetMovieActorIDs(7) → [3, 12, 40]
   │                                        пусто? вернуть [], в сеть не идём
   └─ actorProvider.GetActorsByIDs([3,12,40])
3. ActorsClient.GetActorsByIDs
   ├─ []int → []int64, BatchGetActorsRequest{Ids: [...]}
   └─ c.client.BatchGetActors(ctx, req)
4. grpc-go: применяет service config (дедлайн 2s),
   сериализует protobuf, шлёт HTTP/2 POST
   /actors.v1.ActorService/BatchGetActors
   с заголовком grpc-timeout                        ───────────▶
                                                                   5. grpc.Server принимает,
                                                                      ищет метод в ServiceDesc
                                                                   6. LoggerInterceptor: логгер в ctx
                                                                   7. RecoveryInterceptor: defer recover
                                                                   8. ActorGRPCServer.BatchGetActors
                                                                      ├─ len(ids) > 100? InvalidArgument
                                                                      └─ actorService.GetActorsByIDs
                                                                   9. repository: SELECT … WHERE id = ANY($1)
                                                                  10. []domain.Actor → []*actorsv1.Actor
                                                    ◀───────────  11. ответ; LoggerInterceptor пишет code=OK
12. resp.GetActors() → actorFromProto для каждого
13. []domain.Actor ← сервис ← HTTP-хендлер → JSON пользователю
```

Если actors-service выключен: на шаге 4 соединение не установится → `UNAVAILABLE` → ещё 2 попытки
→ `FromStatus` → `ErrServiceUnavailable` → пользователь получит **503**, а не 500.

---

## 7. Дедлайны: кто кого ограничивает

Дедлайн в gRPC **передаётся по сети**: клиент кладёт в заголовок `grpc-timeout` оставшееся время,
сервер создаёт `ctx` с этим дедлайном. Поэтому цепочка выглядит так:

```
HTTP-запрос в movies-service          ctx (без дедлайна, пока клиент не отключится)
  └─ gRPC-вызов                        дедлайн = min(ctx, 2s из service config)
       └─ сервер actors-service        ctx с тем же дедлайном (пришёл в grpc-timeout)
            └─ repository              context.WithTimeout(ctx, POSTGRES_TIMEOUT) → побеждает меньший
```

Практический вывод: если movies-service перестал ждать (2s прошло), actors-service **тоже** получит
отменённый `ctx`, и pgx прервёт SQL-запрос. Сервер не делает работу, результат которой уже никому не нужен.
Это работает только потому, что `ctx` честно протаскивается через все слои — не заменяй его на `context.Background()`.

---

## 8. Как потрогать руками

```bash
make env-up && make app-run          # оба сервиса; gRPC actors-service на :9090

# какие сервисы есть (работает благодаря reflection)
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext localhost:9090 describe actors.v1.ActorService

# вызовы. Запрос — JSON, grpcurl сам переведёт в protobuf
grpcurl -plaintext -d '{"id": 1}'          localhost:9090 actors.v1.ActorService/GetActor
grpcurl -plaintext -d '{"ids": [1, 2, 999]}' localhost:9090 actors.v1.ActorService/BatchGetActors

# ошибки
grpcurl -plaintext -d '{"id": 999999}' localhost:9090 actors.v1.ActorService/GetActor   # Code: NotFound
```

В ответе `"id": "1"` будет **строкой** — это не баг: по правилам proto3 JSON `int64` кодируется строкой,
потому что JavaScript не умеет точно хранить 64-битные целые.

Эксперименты:
1. Останови actors-service и сделай `GET /api/v1/movies/{id}/actors` — должен быть 503,
   в логах movies-service видно, что ответ пришёл не сразу (ретраи).
2. Добавь `time.Sleep(3 * time.Second)` в `ActorGRPCServer.GetActor` — получишь `DeadlineExceeded` → 503.
   Посмотри в логах actors-service, с каким кодом завершился запрос на сервере.
3. Добавь `panic("boom")` туда же — клиент получит `Internal`, процесс actors-service не упадёт.

Вместо grpcurl можно использовать GUI: Postman и Insomnia умеют gRPC через reflection.

---

## 9. Что в текущем коде можно улучшить

Это не ошибки, которые надо срочно чинить, а места, на которые стоит обратить внимание.

1. **Service config захардкожен.** Таймаут и число попыток нельзя поменять без пересборки.
   Варианты: retry-interceptor из `github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry`
   (типизированные опции, самый распространённый вариант в Go) либо Go-структура с json-тегами,
   заполненная из `Config`, + `json.Marshal`.
2. **`ACTORS_GRPC_SHUTDOWN_TIMEOUT` в конфиге клиента не используется** — остался после копирования серверного
   конфига. Клиенту нужен скорее `Timeout` (вместо `"2s"` в JSON).
3. **Лимит batch знает только сервер.** У фильма больше 100 актёров → сервер вернёт `InvalidArgument`
   → `FromStatus` → `ErrInvalidArgument` → пользователь получит **400** на обычный GET, хотя он ни в чём не виноват.
   Клиент должен либо резать ids на пачки по 100, либо такую ошибку от зависимости отдавать как 500.
   Общее правило: ошибка «плохой аргумент» от **внутреннего** сервиса — это баг вызывающего, а не пользователя.
4. **`Aborted` не обрабатывается в `FromStatus`** — сейчас не страшно (методы только читают), но таблицы
   `ToStatus`/`FromStatus` лучше держать симметричными.
5. **Нет сквозного request-id.** HTTP-middleware `RequestID` в movies-service генерирует id, но в gRPC
   он не передаётся, и логи двух сервисов нельзя связать. Решение: клиентский interceptor кладёт id
   в **metadata** (`metadata.AppendToOutgoingContext`), серверный — достаёт и кладёт в логгер.
   В реальных проектах это делает OpenTelemetry (`otelgrpc`) — вместе с трейсами.
6. **LoggerInterceptor пишет всё в `Debug`**, включая `Internal`. Удобнее выбирать уровень по коду:
   `OK` → Debug, `NotFound`/`InvalidArgument` → Warn, остальное → Error (как в HTTP `ErrorResponse`).
7. **Нет health check.** Стандарт — сервис `grpc.health.v1.Health` (`google.golang.org/grpc/health`),
   его понимают Kubernetes-пробы (`grpc:` probe) и балансировщики.
8. **`google.golang.org/grpc` помечен `// indirect` в `go.mod`**, хотя импортируется напрямую — `go mod tidy` поправит.
9. **Балансировка.** Один `ClientConn` держит одно HTTP/2-соединение. Если actors-service запустить в 3 репликах
   за обычным Kubernetes Service, все запросы уйдут в одну реплику (L4-балансировка видит одно соединение).
   Лечится headless Service + `dns:///` + `round_robin` в service config, либо mesh.

---

## 10. Вопросы для самопроверки

1. Где в проекте объявлен интерфейс, который реализует `ActorGRPCServer`? Что произойдёт, если убрать
   встраивание `UnimplementedActorServiceServer`?
2. Почему `grpc.NewClient` не возвращает ошибку, если actors-service выключен?
3. В каком порядке выполняются interceptors и почему recovery стоит вторым?
4. actors-service ответил `codes.Internal`. Какой HTTP-код получит пользователь movies-service и через какие функции
   пройдёт ошибка?
5. Почему ретраится только `UNAVAILABLE`? Что сломалось бы, если ретраить `DEADLINE_EXCEEDED` для метода,
   который **создаёт** запись?
6. movies-service ждёт 2s, у Postgres в actors-service таймаут 5m. Сколько реально будет выполняться SQL-запрос,
   если база тормозит? Почему?
7. Что нужно изменить и в каких файлах, чтобы добавить метод `ListActorsByLastName`? (Подсказка: proto → генерация →
   адаптер сервера → клиент → `ActorProvider`.)
8. Почему `MovieService` зависит от `ActorProvider`, а не от `*movie_actors_client.ActorsClient`?
