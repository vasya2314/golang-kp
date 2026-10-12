# Постеры фильмов: загрузка и хранение файлов

Разбор того, как принять файл по HTTP, проверить его, положить в объектное хранилище (S3)
и отдать обратно. Всё в стиле существующего кода.

Полный код здесь есть только для того, что встречается впервые: S3-клиент, разбор multipart,
стриминг ответа. Рутинные правки (лишняя колонка в `Scan` и т.п.) описаны словами, их делаешь сам.
В конце каждого раздела есть вопросы для самопроверки.

## 1. Идея

Где хранить картинку?

- **В БД (`BYTEA`).** Работает, но каждый `SELECT *` тащит мегабайты, бэкапы раздуваются,
  а отдача файла занимает соединение к Postgres. Для файлов это плохой вариант.
- **На диске сервера.** Самый простой вариант. Но когда появятся два сервиса или две реплики,
  у каждой будет свой диск: файл загрузили на одну, а запрос пришёл на другую.
  Мы как раз собираемся делить проект на сервисы, так что этот путь сразу отпадает.
- **В объектном хранилище (S3).** Отдельный сервис для файлов, доступный всем репликам.
  Это стандарт индустрии.

Схема:

```
         PUT /movies/1/poster (multipart)
клиент ───────────────────────────────────► golang-kp ──PutObject──► S3 (bucket "posters")
                                                │                    movies/1/poster-<uuid>.jpg
                                                │
                                                └──UPDATE movies SET poster_key = 'movies/1/poster-<uuid>.jpg'
```

**В БД хранится только ключ объекта, а не файл и не URL.** URL зависит от того, как мы файл отдаём
(через приложение, CDN или presigned-ссылку), и может поменяться. Ключ не меняется.

## 2. Что получим в итоге

```
PUT /api/v1/movies/1/poster   multipart, поле poster   → 200 + фильм с poster_url
GET /api/v1/movies/1/poster                            → 200 + байты картинки
GET /api/v1/movies/1                                   → { ..., "poster_url": "/api/v1/movies/1/poster" }
DELETE /api/v1/movies/1                                → фильм удалён, файл в S3 тоже
```

### Почему не загружать постер прямо в `POST /movies`?

Можно сделать `POST /movies` как `multipart/form-data`: JSON в одном поле, файл в другом.
Но так обычно не делают:

- Весь API сейчас на JSON с валидацией через `DecodeAndValidateRequest`. Пришлось бы разбирать
  JSON из поля формы и делать это вторым способом.
- Файл — самая тяжёлая и ненадёжная часть запроса. Если оборвётся загрузка 5 МБ, клиент повторит
  только её, а не создание фильма (иначе получится дубликат фильма).
- `PUT /movies/{id}/poster` одинаково подходит и для «создания», и для «обновления» постера.

Поэтому «создание фильма с постером» превращается в два запроса: `POST /movies`, потом `PUT /movies/{id}/poster`.
Так устроены API большинства сервисов.

### Файлы

```
docker-compose.yaml            ← + S3-сервер
Makefile                       ← + переменные S3, env-up поднимает S3
.env                           ← + S3_*
migrations/
  000005_add_movie_poster.up.sql / .down.sql   ← новые
internal/core/
  domain/movie.go              ← + PosterKey
  domain/file.go               ← новый: domain.File
  errors/common.go             ← + 2 ошибки
  storage/s3/config.go         ← новый
  storage/s3/storage.go        ← новый
  transport/http/response/handler.go  ← + 413, 415
internal/modules/movies/
  repository/postgres/*.go     ← poster_key во всех SELECT/RETURNING
  service/service.go           ← + интерфейс FileStorage
  service/upload_movie_poster.go  ← новый
  service/get_movie_poster.go     ← новый
  service/delete_movie.go         ← удалить файл
  transport/http/upload_movie_poster.go  ← новый
  transport/http/get_movie_poster.go     ← новый
  transport/http/dto_common.go           ← + poster_url
internal/app/app.go            ← собрать всё вместе
```

## 3. Окружение: S3 в docker-compose

### Какой S3-сервер взять локально

Долго стандартом был MinIO, но его Community Edition закрыли: репозиторий архивирован,
а 11 сентября 2026 образы `minio/minio` удалены с Docker Hub. Старые compose-файлы с ним больше не запускаются.

Сейчас чаще всего выбирают одну из трёх замен:

| | Плюсы | Минусы |
|---|---|---|
| **RustFS** | ближе всех к MinIO: один контейнер, веб-консоль, Apache 2.0 | молодой проект, редкие функции S3 ещё догоняют |
| **SeaweedFS** | зрелый, хорошо держит много мелких файлов | несколько компонентов, настройка сложнее |
| **Garage** | самый лёгкий | нет веб-консоли и lifecycle-правил, лицензия AGPL |

Для учёбы берём **RustFS**: он запускается одним контейнером, и в консоли видно, что лежит в бакете.

**Важно:** код ниже не зависит от конкретного сервера. Используется официальный `aws-sdk-go-v2`,
который работает с любым S3-совместимым хранилищем. Чтобы сменить хранилище, достаточно поменять
сервис в compose и `S3_ENDPOINT`, Go-код трогать не нужно. История с MinIO показывает, почему это важно:
код, завязанный на клиент `minio-go`, пришлось бы переписывать.

`docker-compose.yaml`:

```yaml
  golang-kp-s3:
    image: rustfs/rustfs:1.0.0
    container_name: golang-kp-s3
    environment:
      - RUSTFS_VOLUMES=/data
      - RUSTFS_ADDRESS=0.0.0.0:9000
      - RUSTFS_CONSOLE_ENABLE=true
      - RUSTFS_CONSOLE_ADDRESS=0.0.0.0:9001
      - RUSTFS_ACCESS_KEY=${S3_ACCESS_KEY}
      - RUSTFS_SECRET_KEY=${S3_SECRET_KEY}
    volumes:
      - ${PROJECT_ROOT}/out/s3data:/data
    ports:
      - "127.0.0.1:9000:9000"   # S3 API
      - "127.0.0.1:9001:9001"   # веб-консоль, удобно смотреть, что лежит в бакете
    restart: always
```

Для Postgres у тебя сделан `port-forwarder`. Здесь проще сразу опубликовать порты на `127.0.0.1`.
Оба варианта рабочие.

`.env`:

```
S3_ENDPOINT=http://localhost:9000
S3_REGION=us-east-1
S3_ACCESS_KEY=golang_kp
S3_SECRET_KEY=<минимум 8 символов>
S3_BUCKET=posters
```

`Makefile`: в `env-up` и `env-down` добавь `golang-kp-s3`, в `env-cleanup` добавь `rm -rf ${PROJECT_ROOT}/out/s3data`.

Проверка: `make env-up`, затем открой http://localhost:9001 и войди с `S3_ACCESS_KEY` / `S3_SECRET_KEY`.

## 4. Миграция

`make migrate-create seq=add_movie_poster`

```sql
-- 000005_add_movie_poster.up.sql
ALTER TABLE golang_kp.movies ADD COLUMN poster_key TEXT;
```

```sql
-- 000005_add_movie_poster.down.sql
ALTER TABLE golang_kp.movies DROP COLUMN poster_key;
```

`NULL` означает «постера нет». Новая колонка никак не связана с S3: БД не знает, существует ли объект.
За это отвечаем мы в сервисе (см. раздел 8).

## 5. Домен

### `domain.Movie`

Добавь поле `PosterKey *string` в `Movie` и параметр в `NewMovie`.
`NewMovieUninitialized` передаёт `nil`: у нового фильма постера нет.

В `MoviePatch` поле **не** добавляй. Постер меняется только через отдельный эндпоинт,
`PATCH /movies/{id}` не должен уметь записать в `poster_key` произвольную строку.

### `domain.File`

`internal/core/domain/file.go`

```go
package domain

import "io"

// File — содержимое файла и его метаданные.
//
// Content закрывает тот, кто его открыл:
//   - при загрузке файл открыл HTTP-хендлер, он и закрывает;
//   - при чтении из хранилища File возвращается вызывающему, и закрыть Content должен он.
type File struct {
	Content     io.ReadCloser
	Size        int64
	ContentType string
}

func NewFile(content io.ReadCloser, size int64, contentType string) File {
	return File{
		Content:     content,
		Size:        size,
		ContentType: contentType,
	}
}
```

Почему `io.ReadCloser`, а не `[]byte`: файл не нужно целиком держать в памяти, он **стримится**.
Из запроса он читается кусками и кусками же уходит в S3. При 100 одновременных загрузках по 5 МБ
это экономит 500 МБ RAM.

**Самопроверка:** почему у `File` нет поля «имя файла от клиента»? (Подсказка: раздел 9.)

## 6. Ошибки

`internal/core/errors/common.go`:

```go
ErrPayloadTooLarge      = errors.New("слишком большой размер")
ErrUnsupportedMediaType = errors.New("неподдерживаемый тип файла")
```

`internal/core/transport/http/response/handler.go`: добавь в `switch` в `ErrorResponse`:

```go
case errors.Is(err, core_errors.ErrPayloadTooLarge):
	statusCode = http.StatusRequestEntityTooLarge // 413
	logFunc = h.log.Warn

case errors.Is(err, core_errors.ErrUnsupportedMediaType):
	statusCode = http.StatusUnsupportedMediaType // 415
	logFunc = h.log.Warn
```

## 7. Core: S3-хранилище

```
go get github.com/aws/aws-sdk-go-v2/aws \
       github.com/aws/aws-sdk-go-v2/credentials \
       github.com/aws/aws-sdk-go-v2/service/s3
```

Хранилище кладём в `core`, рядом с пулом Postgres. Это инфраструктура, а не логика фильмов,
и потом оно понадобится, например, для фото актёров.

### Конфиг

`internal/core/storage/s3/config.go` — тот же шаблон, что `core_pgx_pool.Config`:

```go
package core_s3_storage

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Endpoint  string        `envconfig:"ENDPOINT" required:"true"`
	Region    string        `envconfig:"REGION" default:"us-east-1"`
	AccessKey string        `envconfig:"ACCESS_KEY" required:"true"`
	SecretKey string        `envconfig:"SECRET_KEY" required:"true"`
	Bucket    string        `envconfig:"BUCKET" required:"true"`
	Timeout   time.Duration `envconfig:"TIMEOUT" default:"30s"`
}
```

`NewConfig` и `NewConfigMust` напиши сам по образцу `core_pgx_pool`, префикс `"S3"`.

### Клиент

`internal/core/storage/s3/storage.go`

```go
package core_s3_storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

type Storage struct {
	client    *s3.Client
	bucket    string
	opTimeout time.Duration
}

func NewStorage(ctx context.Context, config Config) (*Storage, error) {
	client := s3.New(s3.Options{
		Region:       config.Region,
		BaseEndpoint: aws.String(config.Endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			config.AccessKey,
			config.SecretKey,
			"",
		),
		// http://localhost:9000/posters/key вместо http://posters.localhost:9000/key
		UsePathStyle: true,
		// Новые версии SDK по умолчанию шлют CRC-контрольные суммы,
		// которые понимают не все S3-совместимые серверы
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})

	storage := &Storage{
		client:    client,
		bucket:    config.Bucket,
		opTimeout: config.Timeout,
	}

	if err := storage.ensureBucket(ctx); err != nil {
		return nil, fmt.Errorf("подготовка бакета %q: %w", config.Bucket, err)
	}

	return storage, nil
}

// Создаёт бакет, если его ещё нет: на чистом окружении не нужно ничего настраивать руками
func (s *Storage) ensureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}

	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("проверка бакета: %w", err)
	}

	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("создание бакета: %w", err)
	}

	return nil
}

func (s *Storage) Put(ctx context.Context, key string, file domain.File) error {
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          file.Content,
		ContentLength: aws.Int64(file.Size),
		ContentType:   aws.String(file.ContentType),
	})
	if err != nil {
		return fmt.Errorf("загрузка объекта %q: %w", key, err)
	}

	return nil
}

func (s *Storage) Get(ctx context.Context, key string) (domain.File, error) {
	// Здесь нет WithTimeout + defer cancel(): тело объекта читается уже ПОСЛЕ возврата
	// из функции, и cancel() оборвал бы чтение. Время жизни задаёт контекст запроса.
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return domain.File{}, fmt.Errorf("объект %q: %w", key, core_errors.ErrNotFound)
		}

		return domain.File{}, fmt.Errorf("получение объекта %q: %w", key, err)
	}

	return domain.NewFile(
		out.Body,
		aws.ToInt64(out.ContentLength),
		aws.ToString(out.ContentType),
	), nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	// S3 не возвращает ошибку, если объекта нет: удаление идемпотентно
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("удаление объекта %q: %w", key, err)
	}

	return nil
}
```

Что здесь важно:

- **`errors.As` с типами SDK.** Это тот же приём, что с `*pgconn.PgError` в `AddMovieActor`:
  ошибку чужой библиотеки переводим в нашу `core_errors.ErrNotFound`, и выше по стеку никто не знает про S3.
- **`ContentLength`.** S3 должен заранее знать размер. Без него SDK попытается вычитать поток целиком.
- **Подпись запроса.** По `http://` SDK считает SHA-256 тела и для этого перематывает поток.
  `multipart.File` поддерживает `Seek`, поэтому всё работает. По `https://` это не нужно.

**Самопроверка:** что случится, если в `Get` всё-таки написать `defer cancel()`? Проверь на практике:
скачай постер и посмотри на ошибку в логе.

## 8. Репозиторий и сервис

### Репозиторий

1. **`poster_key` во всех запросах.** Добавь `poster_key` в `SELECT` / `RETURNING` в `GetMovie`, `GetMovies`,
   `CreateMovie`, `PatchMovie`, поле `PosterKey *string` в `MovieModel` и в `Scan`.
2. **`PatchMovie` пишет `poster_key`.** Добавь в `SET` строку `poster_key=$N`. Отдельный метод «обновить постер»
   не нужен: сервис загрузит фильм, поменяет `PosterKey` и сохранит через `PatchMovie`.
   Так мы бесплатно получаем **оптимистичную блокировку** по `version`.
3. **`DeleteMovie` возвращает ключ.** Пусть он возвращает ключ постера удалённого фильма:

```go
func (r *MovieRepository) DeleteMovie(ctx context.Context, id int) (*string, error)
```

```sql
DELETE FROM movies WHERE id = $1 RETURNING poster_key;
```

Используй `QueryRow` + `Scan(&posterKey)`, а `pgx.ErrNoRows` превращай в `ErrNotFound`, как в `GetMovie`.
Один запрос вместо пары «сначала SELECT, потом DELETE», и между ними никто не успеет поменять постер.

### Интерфейс хранилища

`internal/modules/movies/service/service.go`:

```go
type FileStorage interface {
	Put(ctx context.Context, key string, file domain.File) error
	Get(ctx context.Context, key string) (domain.File, error)
	Delete(ctx context.Context, key string) error
}

type MovieService struct {
	movieRepository MovieRepository
	fileStorage     FileStorage
}
```

Интерфейс объявлен **в сервисе**, а не в `core/storage`, как и `MovieRepository`:
потребитель описывает, что ему нужно. Поэтому сервис ничего не знает про S3,
а в тестах можно подставить хранилище в памяти.

### Загрузка: порядок операций важен

У нас две системы, Postgres и S3, и общей транзакции у них нет. Любой шаг может упасть.
Разберём варианты:

| Порядок | Если упадёт второй шаг |
|---|---|
| UPDATE в БД → загрузка в S3 | В БД ключ на **несуществующий** файл, у клиента ломаются картинки. ❌ |
| загрузка в S3 → UPDATE в БД | В S3 лишний файл, который никто не видит. Мусор, но не поломка. ✅ |

Отсюда правило: **сначала то, что можно незаметно откатить или оставить мусором, потом то, что видит пользователь.**
А ещё лучше подчистить за собой (компенсация).

`internal/modules/movies/service/upload_movie_poster.go`

```go
package movie_service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/vasya2314/golang-kp/internal/core/domain"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	"go.uber.org/zap"
)

// Какие форматы принимаем и с каким расширением сохраняем
var posterExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

func (s *MovieService) UploadMoviePoster(
	ctx context.Context,
	movieID int,
	poster domain.File,
) (domain.Movie, error) {
	ext, ok := posterExtensions[poster.ContentType]
	if !ok {
		return domain.Movie{}, fmt.Errorf(
			"тип файла %q: %w",
			poster.ContentType,
			core_errors.ErrUnsupportedMediaType,
		)
	}

	movie, err := s.movieRepository.GetMovie(ctx, movieID)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("получение фильма: %w", err)
	}

	// Каждый раз новый ключ, а не перезапись старого — см. ниже
	newKey := fmt.Sprintf("movies/%d/poster-%s%s", movieID, uuid.NewString(), ext)

	if err = s.fileStorage.Put(ctx, newKey, poster); err != nil {
		return domain.Movie{}, fmt.Errorf("сохранение постера: %w", err)
	}

	oldKey := movie.PosterKey
	movie.PosterKey = &newKey

	updatedMovie, err := s.movieRepository.PatchMovie(ctx, movieID, movie)
	if err != nil {
		// Компенсация: БД не обновилась, значит новый файл никому не нужен.
		// WithoutCancel — если клиент оборвал запрос, ctx уже отменён, а убрать за собой всё равно надо.
		s.deletePosterQuietly(context.WithoutCancel(ctx), newKey)

		return domain.Movie{}, fmt.Errorf("сохранение ключа постера: %w", err)
	}

	// Старый файл больше не нужен. Если удалить не вышло, это мусор, а не ошибка запроса.
	if oldKey != nil {
		s.deletePosterQuietly(ctx, *oldKey)
	}

	return updatedMovie, nil
}

func (s *MovieService) deletePosterQuietly(ctx context.Context, key string) {
	if err := s.fileStorage.Delete(ctx, key); err != nil {
		core_logger.FromContext(ctx).Warn(
			"не удалось удалить постер, в хранилище остался мусор",
			zap.String("key", key),
			zap.Error(err),
		)
	}
}
```

**Почему новый ключ (`uuid`) при каждой загрузке, а не перезапись `movies/1/poster.jpg`:**

- Компенсация безопасна: пока БД не обновилась, старый файл цел и клиенты видят старый постер.
  При перезаписи старый файл исчез бы сразу, ещё до UPDATE.
- Два одновременных PUT не перетрут файлы друг друга. Победит один по `version`, второй получит 409,
  а его файл удалит компенсация.
- Если потом поставить CDN, новый URL не нужно сбрасывать из кэша.

**Почему проверка формата здесь, а не в хендлере:** «постер — это jpeg/png/webp» — правило предметной области.
Хендлер только определяет, что за файл пришёл (раздел 9), а решает сервис.

### Получение

`get_movie_poster.go`: напиши сам. Логика такая:

1. `GetMovie` — нет фильма → 404.
2. `movie.PosterKey == nil` → `fmt.Errorf("у фильма id='%d' нет постера: %w", id, core_errors.ErrNotFound)`.
3. `s.fileStorage.Get(ctx, *movie.PosterKey)` → вернуть `domain.File`.

### Удаление фильма

`delete_movie.go`: после успешного `DeleteMovie` из репозитория, если ключ не `nil`, вызови `deletePosterQuietly`.

**Самопроверка:**
1. Почему удаление фильма и его файла идёт в порядке «сначала БД, потом S3», а не наоборот? Сверься с таблицей выше.
2. Лишние файлы всё равно иногда будут оставаться (например, процесс убили между Put и UPDATE).
   Как бы ты их находил и чистил? (Подсказка: сравнить список ключей в бакете с `poster_key` в БД. Это задача для фоновой джобы.)

## 9. Transport: приём и отдача файла

### Как выглядит multipart-запрос

`curl -F 'poster=@matrix.jpg'` отправляет вот такое:

```
POST /api/v1/movies/1/poster
Content-Type: multipart/form-data; boundary=----X

------X
Content-Disposition: form-data; name="poster"; filename="matrix.jpg"
Content-Type: image/jpeg

<байты файла>
------X--
```

**Не доверяй ничему из этих заголовков.** `filename` может быть `../../etc/passwd`,
а `Content-Type: image/jpeg` можно повесить на `.exe`. Поэтому:

- имя файла не используем вообще, ключ генерируем сами (`uuid`);
- тип определяем **по содержимому** через `http.DetectContentType`. Функция смотрит на первые 512 байт:
  у JPEG они начинаются с `FF D8 FF`, у PNG с `89 50 4E 47`.

### Загрузка

`internal/modules/movies/transport/http/upload_movie_poster.go`

```go
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
	// Файл короче 512 байт — это нормально
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("чтение заголовка файла: %w", err)
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("перемотка файла: %w", err)
	}

	return http.DetectContentType(head[:n]), nil
}
```

Разбор:

- **`MaxBytesReader`** — первая линия обороны. Он оборвёт чтение, как только лимит превышен,
  и большой файл не дойдёт ни до памяти, ни до диска.
- **`ParseMultipartForm(1 МБ)`** держит до 1 МБ в памяти, остальное сбрасывает во временный файл.
  Отсюда и `RemoveAll()`: без него `/tmp` постепенно забьётся.
- **`Seek(0)` после `DetectContentType`.** Мы прочитали 512 байт. Если не перемотать,
  в S3 уйдёт картинка без начала, то есть битый файл. Классическая ошибка.

**Самопроверка:** что вернёт сервер, если отправить `curl -F 'poster=@notes.txt'`? На каком слое это решится?

### Отдача

`internal/modules/movies/transport/http/get_movie_poster.go`

```go
func (h *MovieHTTPHandler) GetMoviePoster(w http.ResponseWriter, r *http.Request) {
	// ... ctx, log, responseHandler, разбор id — как в GetMovie ...

	poster, err := h.movieService.GetMoviePoster(ctx, id)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось получить постер")

		return
	}
	defer poster.Content.Close()

	w.Header().Set("Content-Type", poster.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(poster.Size, 10))
	// Браузер не будет «угадывать» тип и не исполнит, например, подсунутый HTML
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	// Байты идут из S3 сразу клиенту, кусками по 32 КБ, без загрузки в память целиком
	if _, err = io.Copy(w, poster.Content); err != nil {
		// Заголовки уже отправлены, ответить JSON-ошибкой нельзя — только залогировать
		log.Warn("обрыв при отдаче постера", zap.Error(err))
	}
}
```

Главное здесь — **после `WriteHeader` уже нельзя вызвать `ErrorResponse`**: статус 200 ушёл клиенту.
Поэтому все проверки (есть ли фильм, есть ли постер, есть ли объект) делаются до первого байта ответа.

### DTO и роуты

`dto_common.go`: добавь `PosterURL *string \`json:"poster_url"\``. Если `movie.PosterKey != nil`, заполни его
строкой `fmt.Sprintf("/api/v1/movies/%d/poster", movie.ID)`, иначе оставь `nil`.
Клиенту отдаём **URL, а не ключ**: ключ — это внутреннее устройство хранилища.

`transport.go`: два метода в интерфейс `MovieService` и роуты:

```go
r.Put("/{id}/poster", h.UploadMoviePoster)
r.Get("/{id}/poster", h.GetMoviePoster)
```

## 10. Сборка в `app.go`

```go
logger.Debug("initializing s3 storage")

fileStorage, err := core_s3_storage.NewStorage(ctx, core_s3_storage.NewConfigMust())
if err != nil {
	logger.Fatal("не удалось инициализировать S3-хранилище", zap.Error(err))
}

// ...
movieService := movie_service.NewMovieService(movieRepository, fileStorage)
```

В `Makefile`, в `app-run`, по аналогии с `POSTGRES_HOST=localhost` ничего менять не нужно:
`S3_ENDPOINT` в `.env` уже указывает на `localhost:9000`.

## 11. Проверка

```bash
make env-up && make migrate-up && make app-run

# фильм
curl -s -X POST localhost:5055/api/v1/movies \
  -d '{"title":"Матрица","release_at":"1999-03-31"}'

# постер
curl -s -X PUT localhost:5055/api/v1/movies/1/poster -F 'poster=@matrix.jpg'
# → { ..., "poster_url": "/api/v1/movies/1/poster" }

# скачать и сравнить с оригиналом — должно совпасть побайтово
curl -s localhost:5055/api/v1/movies/1/poster -o downloaded.jpg
cmp matrix.jpg downloaded.jpg && echo OK
```

Негативные сценарии. Прогони каждый и убедись, что код ответа именно такой:

| Запрос | Ожидаемый ответ |
|---|---|
| `-F 'poster=@notes.txt'` | 415 |
| `head -c 6000000 /dev/urandom > big.bin`, `-F 'poster=@big.bin'` | 413 |
| `-F 'image=@matrix.jpg'` (не то поле) | 400 |
| PUT постера в `/movies/999/poster` | 404 |
| GET постера у фильма без постера | 404 |
| PUT второго постера | 200, в консоли RustFS старый файл исчез |
| DELETE фильма | в консоли RustFS файл исчез |

## 12. Упражнения

От простого к сложному:

1. **`DELETE /movies/{id}/poster`.** Обнулить `poster_key` через `PatchMovie` и удалить объект.
   В каком порядке, и почему не так, как при загрузке?
2. **Проверка размеров картинки.** `image.DecodeConfig` читает только заголовок и возвращает ширину и высоту.
   Отклони постеры меньше 300×450. Не забудь про `Seek(0)` и про `import _ "image/jpeg"`.
3. **`ETag` / `If-None-Match`.** Отдавай `ETag` = ключ объекта, а на совпадение отвечай `304 Not Modified`
   без похода в S3.
4. **Presigned URL.** Вместо проксирования через приложение генерируй временную ссылку прямо на S3
   (`s3.NewPresignClient` → `PresignGetObject`), и клиент качает напрямую.
   Плюсы и минусы по сравнению с текущим вариантом?
5. **Тест сервиса** с фейковым `FileStorage` в памяти. Проверь, что при ошибке `PatchMovie`
   новый файл удаляется (компенсация).

## 13. Как это делают в реальных проектах

Схема выше — **загрузка через бэкенд** (proxy upload). Она реально используется в проде,
когда файлы маленькие (аватарки, постеры до нескольких МБ), а нагрузка умеренная.
Её достоинство в простоте: вся проверка в одном месте.
Но с ростом нагрузки к ней обычно добавляют следующее.

### 13.1. Прямая загрузка в S3 через presigned URL — индустриальный стандарт

Проблема proxy upload: каждый байт файла проходит через наш сервис. 100 пользователей грузят видео,
и все воркеры Go заняты перекачкой байтов, а не логикой. Поэтому крупные проекты
(соцсети, маркетплейсы, облачные диски) делают так:

```
1. POST /movies/1/poster/upload-url          клиент → бэкенд
   ← { "upload_url": "https://s3.../tmp/<uuid>?X-Amz-Signature=...", "upload_id": "<uuid>" }
       бэкенд подписал ссылку: только PUT, только этот ключ, живёт 5 минут,
       Content-Type и максимальный размер зашиты в подпись

2. PUT <upload_url>  (байты файла)           клиент → S3 напрямую, бэкенд не участвует

3. POST /movies/1/poster/confirm { "upload_id": "<uuid>" }    клиент → бэкенд
   бэкенд: HeadObject (файл есть? размер?), проверяет содержимое,
           копирует tmp/<uuid> → movies/1/poster-<uuid>.jpg, сохраняет ключ в БД
```

- Подпись генерирует `s3.NewPresignClient(client).PresignPutObject(...)`, это локальная криптография без сетевого запроса.
- Ограничить размер у presigned **PUT** нельзя. Для этого есть presigned **POST** с policy
  `content-length-range`, или проверка размера через `HeadObject` на шаге 3.
- Отдача тоже идёт мимо бэкенда: presigned GET-ссылка или CDN (раздел 13.3).

### 13.2. Мусор чистит сам S3 — lifecycle rules

Брошенные загрузки (клиент получил ссылку и пропал) и файлы, оставшиеся после падений, никто не чистит руками.
В бакете настраивается правило «удалять всё в `tmp/` старше 24 часов». Его поддерживают AWS S3, Yandex Object Storage и большинство S3-совместимых серверов, но не все (например, у Garage его нет), так что сверяйся с документацией своего хранилища.
Поэтому на шаге 1 файл кладётся в `tmp/`, а «постоянным» становится только после подтверждения.
Компенсации в коде остаются, но lifecycle — страховочная сетка на случай, когда они не сработали.

### 13.3. Отдача через CDN, а не через приложение

`GET /movies/1/poster` через Go-сервис хорош для учёбы, но в проде картинки отдаёт CDN
(CloudFront, Cloudflare, Яндекс CDN), который берёт их из S3. Отсюда два следствия:

- **Ключи неизменяемые (uuid в имени)**, как у нас. Можно ставить `Cache-Control: max-age=31536000, immutable`:
  новый постер — это новый URL, и кэш сбрасывать не нужно.
- В `poster_url` отдаётся полный адрес CDN: `https://cdn.example.com/movies/1/poster-<uuid>.jpg`.
  Базовый URL берётся из конфига (`S3_PUBLIC_BASE_URL`). Именно поэтому в БД хранится ключ, а не URL:
  при переезде на другой CDN меняется одна переменная окружения.

### 13.4. Обработка картинок — асинхронно, через очередь

Оригинал от пользователя может весить 8 МБ в 6000×9000. В проде его не отдают как есть, а генерируют
превью (300×450, 600×900), переводят в WebP/AVIF и удаляют EXIF (там бывают GPS-координаты).
Делать это внутри HTTP-запроса долго, поэтому:

```
confirm → событие PosterUploaded в Kafka → воркер ресайзит → кладёт thumbnails в S3 → обновляет БД
```

Пока воркер работает, у фильма статус постера `processing`. Это прямой мостик к следующему разбору про Kafka.
Для ресайза в Go обычно используют `github.com/disintegration/imaging` (на чистом Go)
или `libvips` через `govips` (в разы быстрее, но нужна C-библиотека).

### 13.5. Безопасность

- **Тип по содержимому, а не по заголовку.** Это уже сделано, и так делают везде.
- **Перекодирование картинки** (decode → encode) — самый надёжный способ убедиться, что это картинка,
  и вырезать из неё встроенный мусор. Его делают в воркере из 13.4.
- **Бакет приватный.** Наружу файлы выдаются только через CDN или presigned-ссылки, без публичного листинга.
- **Антивирус** (ClamAV) для документов от пользователей. Для картинок обычно хватает перекодирования.
- **Отдельный домен для пользовательского контента** (`usercontent.example.com`): даже если кто-то загрузит HTML,
  у него не будет доступа к cookies основного сайта.

### 13.6. Что из этого выбрать для нашего проекта

| Этап | Решение |
|---|---|
| Сейчас (учёба, маленькие файлы) | Proxy upload из разделов 7–9 + новый ключ на каждую загрузку + компенсации |
| Следующий шаг | Presigned PUT + confirm + lifecycle rule на `tmp/` |
| После разделения на сервисы | `PosterUploaded` → Kafka → воркер превью |
| Прод | + CDN, `poster_url` из конфига, immutable-кэш |

Сначала стоит сделать proxy upload: на нём ты разберёшься с multipart, стримингом, `Seek` и лимитами.
С presigned URL эти вещи тоже нужны, просто часть работы переезжает в S3.
Упражнение 4 из раздела 12 — первый шаг к 13.1.

## Что дальше

Следующий шаг — разделение на `movies-service` и `actors-service`.
Постеры тогда останутся в `movies-service`, а `core/storage/s3` будет переиспользован.
Компенсации и «нет общей транзакции» из раздела 8 там встретятся снова, уже между сервисами через Kafka.
