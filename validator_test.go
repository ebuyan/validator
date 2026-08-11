package validator

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type testRequest struct {
	RequiredParam string `json:"required_param" validate:"required"`
}

type testGetRequest struct {
	RequiredParam string `schema:"required_param" validate:"required"`
}

func TestBindJSON_POSTRequiredParam(t *testing.T) {
	Register()

	r, err := http.NewRequest(http.MethodPost, "", strings.NewReader(`{"required_param": ""}`))

	require.NoError(t, err)

	req := testRequest{}
	err = BindJSON(&req, r)
	require.Error(t, err)

	var e Error
	ok := errors.As(err, &e)
	require.True(t, ok)
	require.Equal(t, "required_param=required", e.Error())

	r, err = http.NewRequest(http.MethodPost, "", strings.NewReader(`{"required_param": "val"}`))

	require.NoError(t, err)

	err = BindJSON(&req, r)
	require.NoError(t, err)
	require.Equal(t, "val", req.RequiredParam)
}

func TestBindJSON_GETRequiredParam(t *testing.T) {
	Register()

	r, err := http.NewRequest(http.MethodGet, "required_param=", nil)
	require.NoError(t, err)

	req := testGetRequest{}
	err = BindJSON(&req, r)
	require.Error(t, err)

	var e Error
	ok := errors.As(err, &e)
	require.True(t, ok)
	require.Equal(t, "required_param=required", e.Error())

	r, err = http.NewRequest(http.MethodGet, "?required_param=qwe&path=123", nil)
	require.NoError(t, err)

	err = BindJSON(&req, r)
	require.NoError(t, err)
	require.Equal(t, "qwe", req.RequiredParam)
}

func TestValidate_AnonymousHolderSlice(t *testing.T) {
	Register()

	type elem struct {
		Name string `json:"name" validate:"required"`
	}

	// Слайс заворачивается в анонимный holder с `dive` — namespace validator'а
	// приходит без префикса-имени корневой структуры. Имя поля должно сохраниться.
	holder := struct {
		V []elem `validate:"dive"`
	}{V: []elem{{Name: ""}}}

	err := Validate(&holder)
	require.Error(t, err)

	var e Error
	ok := errors.As(err, &e)
	require.True(t, ok)
	require.Contains(t, e.Error(), "name=required")
}

func TestValidate_NotRegistered(t *testing.T) {
	// Эмулируем «Register не звали»: Validate не должен паниковать на nil v10,
	// а вернуть внятную ошибку. Восстанавливаем валидатор после теста.
	saved := v10
	v10 = nil
	defer func() { v10 = saved; Register() }()

	require.NotPanics(t, func() {
		err := Validate(struct {
			A int `validate:"required"`
		}{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not registered")
	})
}

func TestBuildPath_NumericTokenAgainstStruct(t *testing.T) {
	// Защита-в-глубину: числовой токен против структурного типа не должен
	// приводить к панике Elem() на не-контейнерном типе.
	type S struct {
		A int `json:"a"`
	}

	require.NotPanics(t, func() {
		got := buildPath(reflect.TypeOf(S{}), []string{"0", "a"})
		require.NotEmpty(t, got)
	})
}

type vvNode struct {
	Param     string   `json:"param"     validate:"required"`
	Operators []string `json:"operators" validate:"required,min=1"`
}

func errString(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var e Error
	require.ErrorAs(t, err, &e, "expected validator.Error, got %T: %v", err, err)

	return e.Error()
}

func TestValidateValue(t *testing.T) {
	Register()

	t.Run("struct", func(t *testing.T) {
		// Fields — мапа, порядок в строке не гарантирован: проверяем по подстрокам.
		got := errString(t, ValidateValue(vvNode{}))
		require.Contains(t, got, "param=required")
		require.Contains(t, got, "operators=required")
		require.NoError(t, ValidateValue(vvNode{Param: "country", Operators: []string{"="}}))
	})

	t.Run("slice of structs", func(t *testing.T) {
		require.Equal(t, "0.operators=required", errString(t, ValidateValue([]vvNode{{Param: "country"}})))
		require.NoError(t, ValidateValue([]vvNode{{Param: "country", Operators: []string{"="}}}))
	})

	t.Run("nested slice of structs", func(t *testing.T) {
		require.Equal(t, "0.0.param=required", errString(t, ValidateValue([][]vvNode{{{Operators: []string{"="}}}})))
	})

	t.Run("map of structs", func(t *testing.T) {
		require.Equal(
			t,
			"g.param=required",
			errString(t, ValidateValue(map[string]vvNode{"g": {Operators: []string{"="}}})),
		)
	})

	t.Run("map of slices", func(t *testing.T) {
		require.Equal(
			t,
			"g.0.param=required",
			errString(t, ValidateValue(map[string][]vvNode{"g": {{Operators: []string{"="}}}})),
		)
	})

	t.Run("pointer is dereferenced", func(t *testing.T) {
		require.Equal(t, "0.operators=required", errString(t, ValidateValue(&[]vvNode{{Param: "country"}})))
	})

	t.Run("nil and scalars are no-op", func(t *testing.T) {
		require.NoError(t, ValidateValue((*[]vvNode)(nil)))
		require.NoError(t, ValidateValue(nil))
		require.NoError(t, ValidateValue("just a string"))
		require.NoError(t, ValidateValue(42))
	})

	t.Run("empty slice is valid", func(t *testing.T) {
		require.NoError(t, ValidateValue([]vvNode{}))
	})
}
