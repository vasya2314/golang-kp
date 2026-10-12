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
