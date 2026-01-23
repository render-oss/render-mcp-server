package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/httpcontext"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")

func NewDefaultClient() (*ClientWithResponses, error) {
	apiCfg, err := config.DefaultAPIConfig()
	if err != nil {
		return nil, err
	}
	return clientWithAuth(&http.Client{}, apiCfg)
}

func AddHeaders(ctx context.Context, header http.Header, token string) http.Header {
	hc := httpcontext.FromContext(ctx)
	header = cfg.AddUserAgent(header, hc.UserAgent)
	header.Add("authorization", fmt.Sprintf("Bearer %s", token))
	if hc.ForwardedFor != "" {
		header.Add("X-Forwarded-For", hc.ForwardedFor)
	}
	return header
}

func ErrorFromResponse(v any) error {
	responseErr := firstNonNilErrorField(v)
	if responseErr == nil {
		return nil
	}

	if responseErr.Code == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if responseErr.Code == http.StatusForbidden {
		return ErrForbidden
	}

	if responseErr.Message != nil && *responseErr.Message != "" {
		return fmt.Errorf("received response code %d: %s", responseErr.Code, *responseErr.Message)
	}

	return fmt.Errorf("unknown error")
}

type ErrorWithCode struct {
	Error
	Code int
}

func firstNonNilErrorField(response any) *ErrorWithCode {
	if reflect.TypeOf(response).Kind() == reflect.Ptr {
		return firstNonNilErrorField(reflect.ValueOf(response).Elem().Interface())
	}

	v := reflect.ValueOf(response)

	httpRespField := v.FieldByName("HTTPResponse")
	if !httpRespField.IsValid() {
		return nil
	}
	httpResponse, ok := httpRespField.Interface().(*http.Response)
	if !ok {
		couldNotReadResponse := "could not read HTTP response"
		return &ErrorWithCode{Error: Error{Message: &couldNotReadResponse}}
	}

	if httpResponse.StatusCode < 400 {
		return nil
	}

	body, ok := v.FieldByName("Body").Interface().([]byte)
	if !ok {
		couldNotReadBody := "could not read response body"
		return &ErrorWithCode{Error: Error{Message: &couldNotReadBody}}
	}

	var httpError Error
	if err := json.Unmarshal(body, &httpError); err != nil {
		stringBody := string(body)
		return &ErrorWithCode{Error: Error{Message: &stringBody}, Code: httpResponse.StatusCode}
	}

	return &ErrorWithCode{Error: httpError, Code: httpResponse.StatusCode}
}

func clientWithAuth(httpClient *http.Client, apiCfg config.APIConfig) (*ClientWithResponses, error) {
	insertAuth := func(ctx context.Context, req *http.Request) error {
		req.Header = AddHeaders(ctx, req.Header, authn.APITokenFromContext(ctx))
		return nil
	}

	return NewClientWithResponses(apiCfg.Host, WithRequestEditorFn(insertAuth), WithHTTPClient(httpClient))
}

type paginationParams interface {
	SetCursor(cursor *Cursor)
	SetLimit(int)
}

func ListAll[T any, P paginationParams](ctx context.Context, params P, listPage func(ctx context.Context, params P) ([]T, *Cursor, error)) ([]T, error) {
	limit := 100
	params.SetLimit(limit)

	var res []T
	for {
		page, cursor, err := listPage(ctx, params)
		if err != nil {
			return nil, err
		}

		if len(page) == 0 {
			return res, nil
		}

		res = append(res, page...)

		if len(page) < limit {
			return res, nil
		}
		params.SetCursor(cursor)
	}
}
