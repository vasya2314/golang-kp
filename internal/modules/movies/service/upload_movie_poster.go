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

var posterExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

func (s *MovieService) UploadMoviePoster(
	ctx context.Context,
	movieId int,
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

	movie, err := s.movieRepository.GetMovie(ctx, movieId)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("получение фильма: %w", err)
	}

	// Каждый раз новый ключ, а не перезапись старого — см. ниже
	newKey := fmt.Sprintf("movies/%d/poster-%s%s", movieId, uuid.NewString(), ext)

	if err = s.fileStorage.Put(ctx, newKey, poster); err != nil {
		return domain.Movie{}, fmt.Errorf("сохранение постера: %w", err)
	}

	oldKey := movie.PosterKey
	movie.PosterKey = &newKey

	updatedMovie, err := s.movieRepository.PatchMovie(ctx, movieId, movie)
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
