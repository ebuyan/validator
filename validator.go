package validator

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
)

var v10 *validator.Validate
var schemaDecoder = schema.NewDecoder()
var ValidationMessages = make(map[string]string)

type CustomValidator struct {
	Name    string
	Fn      validator.Func
	Message string
}

func Register(customValidators ...CustomValidator) {
	v10 = validator.New(validator.WithRequiredStructEnabled())
	for _, v := range customValidators {
		_ = v10.RegisterValidation(v.Name, v.Fn)
	}
	for _, v := range customValidators {
		ValidationMessages[v.Name] = v.Message
	}
}

func BindJSON(object any, r *http.Request) error {
	if isFormRequest(r) {
		if isMultiPartRequest(r) {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				return err
			}
		} else {
			if err := r.ParseForm(); err != nil {
				return err
			}
		}
		schemaDecoder.IgnoreUnknownKeys(true)
		if err := schemaDecoder.Decode(object, r.Form); err != nil {
			return err
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(object); err != nil {
			return err
		}
	}

	return validate(object)
}

func isFormRequest(r *http.Request) bool {
	return r.Method == http.MethodGet || isMultiPartRequest(r) || isURLEncodedRequest(r)
}

func isMultiPartRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data")
}

func isURLEncodedRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Type"), "urlencoded")
}

func validate(object any) error {
	err := v10.Struct(object)
	if err == nil {
		return nil
	}

	var validationErrors validator.ValidationErrors
	ok := errors.As(err, &validationErrors)
	if !ok {
		return Error{
			Msg: err.Error(),
		}
	}

	if len(validationErrors) == 0 {
		return nil
	}

	fieldErrors := map[string]string{}
	for _, fieldErr := range validationErrors {
		key := buildPath(reflect.TypeOf(object).Elem(), prepareNamespace(fieldErr.Namespace()))

		// Если есть кастомное сообщение — используем его
		message, ok := ValidationMessages[fieldErr.Tag()]
		if ok && message != "" {
			fieldErrors[key] = message
			fmt.Printf("Custom message found: key=%s, message=%s\n", key, message)

			continue
		}

		// Если сообщения нет — формируем стандартный формат
		fieldErrors[key] = fieldErr.Tag()
		if fieldErr.Param() != "" {
			fieldErrors[key] += "=" + fieldErr.Param()
		}
	}

	return Error{
		Fields: fieldErrors,
	}
}

func buildPath(objectType reflect.Type, namespace []string) string {
	field := namespace[0]
	if _, err := strconv.Atoi(field); err == nil {
		if len(namespace) > 1 {
			return field + "." + buildPath(objectType.Elem(), namespace[1:])
		}
		return field
	}

	var f reflect.StructField
	if objectType.Kind() == reflect.Ptr {
		f, _ = objectType.Elem().FieldByName(field)
	} else {
		f, _ = objectType.FieldByName(field)
	}

	tag := getJSONTag(f.Tag)
	path := tag

	if len(namespace) > 1 {
		path += "." + buildPath(f.Type, namespace[1:])
	}

	return path
}

func prepareNamespace(namespace string) []string {
	namespace = strings.SplitN(namespace, ".", 2)[1]
	namespace = strings.ReplaceAll(strings.ReplaceAll(namespace, "[", "."), "]", "")

	return strings.Split(namespace, ".")
}

func getJSONTag(tag reflect.StructTag) string {
	if val, ok := tag.Lookup("schema"); ok {
		return val
	}
	return strings.Split(tag.Get("json"), ",")[0]
}
