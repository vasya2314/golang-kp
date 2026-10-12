# Many-to-many: фильмы ↔ актёры

Разбор того, как в этом проекте сделать связь «многие-ко-многим» между `movies` и `actors`:
от таблицы в БД до HTTP-хендлеров, в стиле существующего кода.

## 1. Идея

У одного фильма много актёров, и один актёр снимается во многих фильмах.

Хранить связь в одной из существующих таблиц нельзя:

- **Колонка `actor_id` в `movies`** вмещает только одного актёра на фильм, это связь «один-ко-многим».
- **Колонка `movie_id` в `actors`** даёт актёру только один фильм.
- **Список в колонке** (`actor_ids = '1,5,7'` или массив): с ним не работают внешние ключи и индексы, а запрос «фильмы актёра 5» становится неудобным и медленным.

Поэтому заводят третью таблицу: каждая её строка — одна пара «этот актёр снимался в этом фильме».

```
movies                movie_actors              actors
┌────┬──────────┐     ┌──────────┬──────────┐   ┌────┬───────────┐
│ id │ title    │     │ movie_id │ actor_id │   │ id │ name      │
├────┼──────────┤     ├──────────┼──────────┤   ├────┼───────────┤
│ 1  │ Матрица  │◄────│    1     │    10    │──►│ 10 │ Киану     │
│ 2  │ Джон Уик │◄────│    1     │    11    │──►│ 11 │ Кэрри-Энн │
└────┴──────────┘ ◄───│    2     │    10    │   └────┴───────────┘
                      └──────────┴──────────┘
```

Киану (10) связан с двумя фильмами, у «Матрицы» (1) два актёра. Сами `movies` и `actors` не меняются.

## 2. Что получим в итоге

```
PUT    /api/v1/movies/1/actors/10   → привязать актёра 10 к фильму 1   → 204
DELETE /api/v1/movies/1/actors/10   → отвязать                         → 204
GET    /api/v1/movies/1/actors      → список актёров фильма 1          → 200 [...]
```

Новые файлы появляются только в модуле `movies`, потому что это «актёры фильма». Модуль `actors` не меняется.

```
migrations/
  000004_add_movie_actors.up.sql
  000004_add_movie_actors.down.sql
internal/modules/movies/
  repository/postgres/
    add_movie_actor.go        ← новый
    remove_movie_actor.go     ← новый
    get_movie_actors.go       ← новый
  service/
    add_movie_actor.go        ← новый
    remove_movie_actor.go     ← новый
    get_movie_actors.go       ← новый
    service.go                ← +3 метода в интерфейс MovieRepository
  transport/http/
    add_movie_actor.go        ← новый
    remove_movie_actor.go     ← новый
    get_movie_actors.go       ← новый
    transport.go              ← +3 метода в интерфейс MovieService, +3 роута
```

## 3. Миграция

`make migrate-create seq=add_movie_actors`

```sql
-- 000004_add_movie_actors.up.sql
CREATE TABLE golang_kp.movie_actors (
    movie_id INT NOT NULL REFERENCES golang_kp.movies(id) ON DELETE CASCADE,
    actor_id INT NOT NULL REFERENCES golang_kp.actors(id) ON DELETE CASCADE,
    PRIMARY KEY (movie_id, actor_id)
);

CREATE INDEX movie_actors_actor_id_idx ON golang_kp.movie_actors (actor_id);
```

```sql
-- 000004_add_movie_actors.down.sql
DROP TABLE IF EXISTS golang_kp.movie_actors;
```

| Что | Зачем |
|---|---|
| `REFERENCES ...` | Внешний ключ: БД не даст связать с несуществующим фильмом или актёром. |
| `ON DELETE CASCADE` | При удалении фильма или актёра связи удаляются сами. Без этого удаление упадёт с ошибкой, пока связи есть. |
| `PRIMARY KEY (movie_id, actor_id)` | Одну пару нельзя добавить дважды. Заодно это индекс для запроса «актёры фильма X». |
| индекс по `actor_id` | Для обратного запроса «фильмы актёра Y»: первичный ключ начинается с `movie_id` и для поиска по `actor_id` не помогает. |

## 4. Репозиторий: SQL и перевод ошибок БД

### Привязка

`internal/modules/movies/repository/postgres/add_movie_actor.go`

```go
package movie_postgres_repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

// Код ошибки Postgres «нарушение внешнего ключа»
const pgForeignKeyViolation = "23503"

func (r *MovieRepository) AddMovieActor(ctx context.Context, movieID, actorID int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	INSERT INTO movie_actors (movie_id, actor_id)
	VALUES ($1, $2)
	ON CONFLICT (movie_id, actor_id) DO NOTHING;
	`

	_, err := r.pool.Exec(ctx, query, movieID, actorID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return fmt.Errorf(
				"фильм id='%d' или актер/актриса id='%d': %w",
				movieID,
				actorID,
				core_errors.ErrNotFound,
			)
		}

		return fmt.Errorf("выполнение запроса: %w", err)
	}

	return nil
}
```

- **`ON CONFLICT DO NOTHING`.** Если связь уже есть, запрос ничего не делает вместо ошибки, поэтому повторный PUT безопасен.
- **Ошибка `23503`.** Если фильма или актёра нет, внешний ключ откажет. Эту ошибку превращаем в `ErrNotFound`, а `ErrorResponse` делает из неё 404. Отдельный SELECT для проверки существования не нужен, это делает БД.
- **`errors.As`** достаёт из ошибки pgx тип `*pgconn.PgError`, чтобы прочитать код.

### Отвязка

`internal/modules/movies/repository/postgres/remove_movie_actor.go`

```go
package movie_postgres_repository

import (
	"context"
	"fmt"

	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

func (r *MovieRepository) RemoveMovieActor(ctx context.Context, movieID, actorID int) error {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `DELETE FROM movie_actors WHERE movie_id = $1 AND actor_id = $2;`

	cmdTag, err := r.pool.Exec(ctx, query, movieID, actorID)
	if err != nil {
		return fmt.Errorf("выполнение запроса: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"связь фильма id='%d' с актером/актрисой id='%d': %w",
			movieID,
			actorID,
			core_errors.ErrNotFound,
		)
	}

	return nil
}
```

Тот же приём, что в `DeleteMovie`: если ничего не удалено, связи не было, клиент получит 404.

### Список актёров фильма

`internal/modules/movies/repository/postgres/get_movie_actors.go`

```go
package movie_postgres_repository

import (
	"context"
	"fmt"
	"time"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

// Своя модель: пакет actor_postgres_repository из модуля actors не импортируем
type movieActorModel struct {
	ID          int
	Version     int
	FirstName   string
	LastName    string
	MiddleName  *string
	Description *string
	BirthDate   time.Time
}

func (r *MovieRepository) GetMovieActors(ctx context.Context, movieID int) ([]domain.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.pool.OpTimeout())
	defer cancel()

	query := `
	SELECT a.id, a.version, a.first_name, a.last_name, a.middle_name, a.description, a.birth_date
	FROM movie_actors ma
	JOIN actors a ON a.id = ma.actor_id
	WHERE ma.movie_id = $1
	ORDER BY a.last_name, a.first_name;
	`

	rows, err := r.pool.Query(ctx, query, movieID)
	if err != nil {
		return nil, fmt.Errorf("выборка актеров фильма: %w", err)
	}
	defer rows.Close()

	actors := make([]domain.Actor, 0)

	for rows.Next() {
		var m movieActorModel

		if err = rows.Scan(
			&m.ID,
			&m.Version,
			&m.FirstName,
			&m.LastName,
			&m.MiddleName,
			&m.Description,
			&m.BirthDate,
		); err != nil {
			return nil, fmt.Errorf("сканирование актеров фильма: %w", err)
		}

		actors = append(actors, domain.NewActor(
			m.ID,
			m.Version,
			m.FirstName,
			m.LastName,
			m.MiddleName,
			m.Description,
			m.BirthDate,
		))
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("следующая строка: %w", err)
	}

	return actors, nil
}
```

- **`JOIN`** идёт по строкам `movie_actors` с нужным `movie_id` и к каждой подтягивает полную запись из `actors`.
- Наружу уходит `[]domain.Actor`: это общий тип из `core/domain`, поэтому модули не зависят друг от друга.
- `make(..., 0)` вместо `var actors []domain.Actor` нужен, чтобы пустой результат стал JSON `[]`, а не `null`.

## 5. Сервис: бизнес-правила

`internal/modules/movies/service/service.go`: добавить методы в интерфейс `MovieRepository`.

```go
type MovieRepository interface {
	// ... существующие методы ...

	AddMovieActor(ctx context.Context, movieID, actorID int) error
	RemoveMovieActor(ctx context.Context, movieID, actorID int) error
	GetMovieActors(ctx context.Context, movieID int) ([]domain.Actor, error)
}
```

`add_movie_actor.go` и `remove_movie_actor.go` — простые пробросы, как `DeleteMovie`:

```go
func (s *MovieService) AddMovieActor(ctx context.Context, movieID, actorID int) error {
	err := s.movieRepository.AddMovieActor(ctx, movieID, actorID)
	if err != nil {
		return fmt.Errorf("привязка актера/актрисы к фильму: %w", err)
	}

	return nil
}
```

`get_movie_actors.go` содержит правило:

```go
package movie_service

import (
	"context"
	"fmt"

	"github.com/vasya2314/golang-kp/internal/core/domain"
)

func (s *MovieService) GetMovieActors(ctx context.Context, movieID int) ([]domain.Actor, error) {
	// Без этой проверки для несуществующего фильма вернётся [] вместо 404
	if _, err := s.movieRepository.GetMovie(ctx, movieID); err != nil {
		return nil, fmt.Errorf("получение фильма: %w", err)
	}

	actors, err := s.movieRepository.GetMovieActors(ctx, movieID)
	if err != nil {
		return nil, fmt.Errorf("получение актеров фильма: %w", err)
	}

	return actors, nil
}
```

Правило «нет фильма — это 404, а не пустой список» относится к бизнес-логике, поэтому живёт в сервисе, а не в SQL.

## 6. Transport: HTTP

`internal/modules/movies/transport/http/transport.go`: методы в интерфейс `MovieService` и роуты.

```go
type MovieService interface {
	// ... существующие методы ...

	AddMovieActor(ctx context.Context, movieID, actorID int) error
	RemoveMovieActor(ctx context.Context, movieID, actorID int) error
	GetMovieActors(ctx context.Context, movieID int) ([]domain.Actor, error)
}

func (h *MovieHTTPHandler) Routes(router *chi.Mux) {
	router.Route("/movies", func(r chi.Router) {
		r.Post("/", h.CreateMovie)
		r.Get("/", h.GetMovies)
		r.Delete("/{id}", h.DeleteMovie)
		r.Get("/{id}", h.GetMovie)
		r.Patch("/{id}", h.PatchMovie)

		r.Get("/{id}/actors", h.GetMovieActors)
		r.Put("/{id}/actors/{actorId}", h.AddMovieActor)
		r.Delete("/{id}/actors/{actorId}", h.RemoveMovieActor)
	})
}
```

`add_movie_actor.go`:

```go
package movie_transport_http

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	core_http_response "github.com/vasya2314/golang-kp/internal/core/transport/http/response"
)

func (h *MovieHTTPHandler) AddMovieActor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	movieID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		err = fmt.Errorf("получение `id`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр id")

		return
	}

	actorID, err := strconv.Atoi(chi.URLParam(r, "actorId"))
	if err != nil {
		err = fmt.Errorf("получение `actorId`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр actorId")

		return
	}

	err = h.movieService.AddMovieActor(ctx, movieID, actorID)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось привязать актера/актрису к фильму")

		return
	}

	responseHandler.NoContentResponse()
}
```

`RemoveMovieActor` такой же, только вызывает `h.movieService.RemoveMovieActor`.

`get_movie_actors.go`:

```go
package movie_transport_http

// импорты как в add_movie_actor.go + "github.com/vasya2314/golang-kp/internal/core/domain"

// Свой DTO: ActorDTOResponse лежит в transport-пакете модуля actors, его не импортируем
type MovieActorDTOResponse struct {
	ID         int     `json:"id"`
	FirstName  string  `json:"first_name"`
	LastName   string  `json:"last_name"`
	MiddleName *string `json:"middle_name"`
	BirthDate  string  `json:"birth_date"`
}

type GetMovieActorsResponse []MovieActorDTOResponse

func (h *MovieHTTPHandler) GetMovieActors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := core_logger.FromContext(ctx)
	responseHandler := core_http_response.NewHTTPResponseHandler(log, w)

	movieID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		err = fmt.Errorf("получение `id`: %w", core_errors.ErrInvalidArgument)
		responseHandler.ErrorResponse(err, "некорректный параметр id")

		return
	}

	actors, err := h.movieService.GetMovieActors(ctx, movieID)
	if err != nil {
		responseHandler.ErrorResponse(err, "не удалось получить актеров фильма")

		return
	}

	response := make(GetMovieActorsResponse, len(actors))
	for i, a := range actors {
		response[i] = MovieActorDTOResponse{
			ID:         a.ID,
			FirstName:  a.FirstName,
			LastName:   a.LastName,
			MiddleName: a.MiddleName,
			BirthDate:  a.BirthDate.Format(domain.DateLayout),
		}
	}

	responseHandler.JSONResponse(response, http.StatusOK)
}
```

DTO свой, а не `ActorDTOResponse`, потому что модули не должны импортировать друг друга. К тому же в списке актёров фильма обычно нужно меньше полей (без `version` и `description`): это другая форма ответа, а не дублирование.

`app.go` менять не нужно: обработчик фильмов уже создан и подключён.

## 7. Как проходит запрос

**Успешная привязка:** `PUT /api/v1/movies/1/actors/10`

```
chi: id="1", actorId="10"
 → transport.AddMovieActor: Atoi → 1, 10
 → service.AddMovieActor
 → repository.AddMovieActor: INSERT INTO movie_actors VALUES (1, 10)
 ← nil
← 204 No Content
```

**Актёра не существует:** `PUT /api/v1/movies/1/actors/999`

```
 → repository: INSERT (1, 999)
   Postgres: FK actor_id → actors(id) нарушен, код 23503
   errors.As → pgErr.Code == "23503" → оборачиваем ErrNotFound
 ← service добавляет контекст: "привязка ...: фильм ... : не найдено"
← ErrorResponse: errors.Is(err, ErrNotFound) → 404
```

**Список актёров несуществующего фильма:** `GET /api/v1/movies/999/actors`

```
 → service.GetMovieActors
   → repository.GetMovie(999) → pgx.ErrNoRows → ErrNotFound
 ← 404, до JOIN-запроса дело не доходит
```

Цепочка `%w` между слоями сохраняет исходную `ErrNotFound`. Поэтому `errors.Is` в `ErrorResponse` находит её через все обёртки и выбирает правильный HTTP-код.

## 8. Правила, которые стоит держать в голове

- **Не встраивай `Actors []Actor` в `domain.Movie`.** Иначе каждый `GetMovie`, `GetMovies` и `PatchMovie` будет вынужден загружать актёров, даже когда они не нужны. Актёров фильма отдаёт отдельный метод.
- **Модули не импортируют друг друга.** Общее (`domain.Actor`) лежит в `core/domain`. Модели БД и DTO у каждого модуля свои.
- **Существование записей проверяет БД** через внешние ключи. Ошибку `23503` переводим в `ErrNotFound`.
- **N+1.** Если понадобится список фильмов вместе с актёрами, не вызывай `GetMovieActors` в цикле: на 50 фильмов это 51 запрос. Сделай два запроса: фильмы, затем актёры для всех них сразу через `WHERE ma.movie_id = ANY($1)`, где `$1` — это `[]int`. Потом разложи результат в `map[int][]domain.Actor` по `movie_id`.
- **Транзакции.** Если появится «заменить весь состав фильма» (`PUT /movies/{id}/actors` со списком), делай `DELETE` старых связей и `INSERT` новых в одной транзакции: `pool.Begin(ctx)` → … → `tx.Commit(ctx)`.

## 9. Как добавить роль (`character_name`)

Роль — свойство пары, а не фильма и не актёра: Киану играл Нео в «Матрице» и Джона в «Джоне Уике».

1. В миграции добавить колонку `character_name VARCHAR(100)`.
2. PUT принимает тело `{"character_name": "Нео"}`, его декодируют через `DecodeAndValidateRequest`.
3. В INSERT заменить `DO NOTHING` на `DO UPDATE SET character_name = EXCLUDED.character_name`, чтобы повторный PUT обновлял роль.
4. В `GetMovieActors` добавить `ma.character_name` в SELECT, в модель и в DTO. В домене понадобится тип вроде `domain.MovieActor{Actor Actor; CharacterName *string}`, потому что у `domain.Actor` роли нет.

## 10. Обратная сторона

`GET /api/v1/actors/{id}/movies` делается так же, но в модуле `actors`: `JOIN movies m ON m.id = ma.movie_id WHERE ma.actor_id = $1`. Для этого запроса и нужен индекс `movie_actors_actor_id_idx`.
