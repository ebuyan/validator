package validator

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
)

var (
	v10                *validator.Validate
	schemaDecoder      = schema.NewDecoder()
	validationMessages = make(map[string]string)
)

type CustomValidator struct {
	Name    string
	Fn      validator.Func
	Message string
}

func Register(customValidators ...CustomValidator) {
	v10 = validator.New(validator.WithRequiredStructEnabled())
	schemaDecoder.IgnoreUnknownKeys(true)
	validationMessages = make(map[string]string)
	for _, v := range customValidators {
		_ = v10.RegisterValidation(v.Name, v.Fn)
		validationMessages[v.Name] = v.Message
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
		if err := schemaDecoder.Decode(object, r.Form); err != nil {
			return err
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(object); err != nil {
			return err
		}
	}

	return Validate(object)
}

func Validate(object any) error {
	if v10 == nil {
		return Error{Msg: "validator is not registered: call Register first"}
	}

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

	rootType := reflect.TypeOf(object)
	if rootType.Kind() == reflect.Ptr {
		rootType = rootType.Elem()
	}

	fieldErrors := map[string]string{}
	for _, fieldErr := range validationErrors {
		key := buildPath(rootType, prepareNamespace(fieldErr.Namespace(), rootType.Name()))

		if message := validationMessages[fieldErr.Tag()]; message != "" {
			// Кастомное сообщение для тега, если зарегистрировано.
			fieldErrors[key] = message
			continue
		}

		fieldErrors[key] = fieldErr.Tag()
		if fieldErr.Param() != "" {
			fieldErrors[key] += "=" + fieldErr.Param()
		}
	}

	return Error{
		Fields: fieldErrors,
	}
}

// ValidateValue валидирует произвольное значение верхнего уровня — структуру,
// слайс, массив или мапу — по тегам `validate`. v10.Struct (и потому Validate)
// принимает только структуры, поэтому не-структурные значения заворачиваются в
// анонимный holder с цепочкой `dive` по числу вложенных уровней — так теги
// проверяются и на конечных элементах (в т.ч. [][]T, map[K][]T). Пути ошибок
// формируются тем же buildPath, что и для обычных структур, поэтому ошибки
// выглядят как "0.field", "0.0.field", "key.field" и т.п. Указатели
// разыменовываются; nil и скалярные значения — no-op.
func ValidateValue(value any) error {
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	// Классификация kind, а не enum-диспетчер: прочие виды — no-op через default.
	//exhaustive:ignore
	switch rv.Kind() {
	case reflect.Struct:
		return Validate(value)
	case reflect.Slice, reflect.Array, reflect.Map:
		holderType := reflect.StructOf([]reflect.StructField{{
			Name: "V",
			Type: rv.Type(),
			Tag:  reflect.StructTag(`validate:"` + diveChain(rv.Type()) + `"`),
		}})

		holder := reflect.New(holderType)
		holder.Elem().Field(0).Set(rv)

		return Validate(holder.Interface())
	default:
		return nil
	}
}

// diveChain строит "dive,dive,..." по числу вложенных слайсов/массивов/мап в t,
// чтобы валидатор дошёл до тегов на конечных элементах.
func diveChain(t reflect.Type) string {
	depth := 0
	for t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		depth++
		t = t.Elem()
	}
	if depth == 0 {
		return ""
	}

	return strings.Repeat("dive,", depth-1) + "dive"
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

func buildPath(objectType reflect.Type, namespace []string) string {
	if len(namespace) == 0 {
		return ""
	}

	if objectType != nil && objectType.Kind() == reflect.Ptr {
		objectType = objectType.Elem()
	}

	field := namespace[0]

	// Индекс слайса/массива или ключ мапы — это не имя поля структуры:
	// сохраняем токен как есть и спускаемся в тип элемента.
	_, idxErr := strconv.Atoi(field)
	isMapKey := objectType != nil && objectType.Kind() == reflect.Map
	if idxErr == nil || isMapKey {
		if len(namespace) > 1 {
			var elem reflect.Type
			// Elem() валиден только для slice/array/map/ptr/chan — иначе паника.
			// Если тип рассинхронизирован с namespace, спускаемся с nil (токены
			// дальше отдадутся как есть).
			if objectType != nil && isElemable(objectType.Kind()) {
				elem = objectType.Elem()
			}

			return field + "." + buildPath(elem, namespace[1:])
		}

		return field
	}

	// Тип не структура (или неизвестен) — резолвить имя поля нечем,
	// отдаём оставшиеся токены как есть, чтобы не паниковать на FieldByName.
	if objectType == nil || objectType.Kind() != reflect.Struct {
		return strings.Join(namespace, ".")
	}

	f, _ := objectType.FieldByName(field)
	tag := getJSONTag(f.Tag)
	path := tag

	if len(namespace) > 1 {
		rest := buildPath(f.Type, namespace[1:])
		// У безымянных полей-обёрток (напр. holder с `dive`) json-тег пустой —
		// не приклеиваем ведущую точку, чтобы путь не начинался с ".".
		if path == "" {
			return rest
		}
		path += "." + rest
	}

	return path
}

func prepareNamespace(namespace, rootName string) []string {
	// Срезаем имя корневой структуры, если оно есть. У анонимной структуры
	// (rootName == "") validator формирует namespace уже без префикса-имени —
	// тогда срезать ничего нельзя, иначе потеряется первое реальное поле.
	if rootName != "" {
		namespace = strings.TrimPrefix(namespace, rootName)
		namespace = strings.TrimPrefix(namespace, ".")
	}

	namespace = strings.ReplaceAll(strings.ReplaceAll(namespace, "[", "."), "]", "")

	return strings.Split(namespace, ".")
}

// isElemable сообщает, есть ли у типа с таким kind метод-безопасный Elem().
func isElemable(k reflect.Kind) bool {
	// Интересуют только контейнерные виды, у которых Elem() валиден.
	//exhaustive:ignore
	switch k {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Ptr, reflect.Chan:
		return true
	default:
		return false
	}
}

func getJSONTag(tag reflect.StructTag) string {
	if val, ok := tag.Lookup("schema"); ok {
		return val
	}

	return strings.Split(tag.Get("json"), ",")[0]
}
