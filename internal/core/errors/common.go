package core_errors

import "errors"

var (
	ErrNotFound                = errors.New("не найдено")
	ErrBadRequest              = errors.New("некорректный запрос")
	ErrUnauthorized            = errors.New("не авторизован")
	ErrForbidden               = errors.New("доступ запрещён")
	ErrInternalServerError     = errors.New("внутренняя ошибка сервера")
	ErrNotAcceptable           = errors.New("неприемлемый формат")
	ErrRequestTimeout          = errors.New("истекло время ожидания запроса")
	ErrConflict                = errors.New("конфликт")
	ErrPreconditionFailed      = errors.New("не выполнено предварительное условие")
	ErrTooManyRequests         = errors.New("слишком много запросов")
	ErrInternalServer          = errors.New("внутренняя ошибка сервера")
	ErrNotImplemented          = errors.New("не реализовано")
	ErrBadGateway              = errors.New("ошибка шлюза")
	ErrServiceUnavailable      = errors.New("сервис недоступен")
	ErrGatewayTimeout          = errors.New("истекло время ожидания шлюза")
	ErrHTTPVersionNotSupported = errors.New("версия HTTP не поддерживается")
	ErrVariantAlsoNegotiates   = errors.New("вариант тоже участвует в согласовании")
	ErrInsufficientStorage     = errors.New("недостаточно места в хранилище")
	ErrLoopDetected            = errors.New("обнаружен цикл")
	ErrInvalidArgument         = errors.New("некорректный аргумент")
)
