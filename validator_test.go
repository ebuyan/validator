package validator

import (
	"errors"
	"net/http"
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
