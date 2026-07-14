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
	"github.com/render-oss/render-mcp-server/pkg/logging"
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

// BodyFromResponse returns the parsed success body from a generated client
// response. It returns an error if the API responded with an error status, or
// with a success status whose body the client did not parse (for example a
// 202 with an empty body where a 201 was expected). This keeps callers from
// silently passing a nil body along.
func BodyFromResponse[T any](body *T, resp interface{ StatusCode() int }) (*T, error) {
	if err := ErrorFromResponse(resp); err != nil {
		return nil, err
	}
	if body == nil {
		err := fmt.Errorf("received response code %d with an unexpected empty body", resp.StatusCode())
		logging.Error("render api: %v", err)
		return nil, err
	}
	return body, nil
}

func ErrorFromResponse(v any) error {
	responseErr := firstNonNilErrorField(v)
	if responseErr == nil {
		return nil
	}

	if responseErr.Code == http.StatusUnauthorized {
		logging.Error("render api: unauthorized (status 401)")
		return ErrUnauthorized
	}
	if responseErr.Code == http.StatusForbidden {
		logging.Error("render api: forbidden (status 403)")
		return ErrForbidden
	}

	var err error
	if responseErr.Message != nil && *responseErr.Message != "" {
		err = fmt.Errorf("received response code %d: %s", responseErr.Code, *responseErr.Message)
	} else {
		err = fmt.Errorf("received response code %d with empty message", responseErr.Code)
	}
	logging.Error("render api: %v", err)
	return err
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
