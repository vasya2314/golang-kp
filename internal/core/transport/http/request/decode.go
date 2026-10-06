package core_http_request

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

var requestValidator = newRequestValidator()

// По умолчанию fe.Field() вернёт ReleaseAt, а хочется release_at. Для этого один раз при создании валидатора регистрируется функция, которая берёт имя из тега json
func newRequestValidator() *validator.Validate {
	v := validator.New()

	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}

		return name
	})

	return v
}

func DecodeAndValidateRequest(r *http.Request, dest any) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return fmt.Errorf(
			"декодирование JSON %v: %w",
			err,
			core_errors.ErrInvalidArgument,
		)
	}

	if err := requestValidator.Struct(dest); err != nil {
		ve, _ := err.(validator.ValidationErrors)

		// len = 0 — элементов пока нет
		// cap = len(ve) — заранее резервируешь память под нужное количество элементов
		fields := make([]FieldError, 0, len(ve))

		for _, fe := range ve {
			msg := "недопустимое значение"

			switch fe.Tag() {
			case "required":
				msg = "поле обязательно для заполнения"
			case "datetime":
				msg = fmt.Sprintf("должно быть датой в формате %s", fe.Param())
			case "min":
				msg = fmt.Sprintf("минимальное значение/длина — %s", fe.Param())
			}

			fields = append(fields, NewFieldError(fe.Field(), msg))
		}
		
		return NewValidationError(fields)
	}

	return nil
}
