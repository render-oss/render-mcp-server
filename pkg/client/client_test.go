package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
)

func TestAddHeadersAPIAuthToken(t *testing.T) {
	enableOAuth := func(t *testing.T) {
		t.Setenv("OAUTH_ENABLED", "true")
		t.Setenv("OAUTH_AUTHORIZATION_SERVER_URL", "https://api.example.com")
		t.Setenv("OAUTH_CANONICAL_RESOURCE_URI", "https://mcp.example.com/mcp")
	}

	t.Run("send the API auth token when OAuth is enabled", func(t *testing.T) {
		enableOAuth(t)
		t.Setenv(oauth.AuthTokenEnv, "api-auth-token")

		h := client.AddHeaders(context.Background(), http.Header{}, "user-token")

		require.Equal(t, "api-auth-token", h.Get("Render-MCP-Auth"))
		require.Equal(t, "Bearer user-token", h.Get("Authorization"))
	})

	t.Run("doesn't send an API auth header when OAuth is disabled", func(t *testing.T) {
		t.Setenv("OAUTH_ENABLED", "false")
		t.Setenv(oauth.AuthTokenEnv, "api-auth-token")

		h := client.AddHeaders(context.Background(), http.Header{}, "user-token")

		require.Empty(t, h.Values("Render-MCP-Auth"))
	})

	t.Run("doesn't send an API auth header when the token is unset", func(t *testing.T) {
		enableOAuth(t)
		t.Setenv(oauth.AuthTokenEnv, "")

		h := client.AddHeaders(context.Background(), http.Header{}, "user-token")

		require.Empty(t, h.Values("Render-MCP-Auth"))
	})
}

func TestErrorFromResponse(t *testing.T) {
	t.Run("status code 401", func(t *testing.T) {
		err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
			Body:         []byte("unauthorized"),
			HTTPResponse: &http.Response{StatusCode: 401},
		})

		require.ErrorIs(t, err, client.ErrUnauthorized)
	})
	t.Run("status code 403", func(t *testing.T) {
		err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
			Body:         []byte("forbidden"),
			HTTPResponse: &http.Response{StatusCode: 403},
		})

		require.ErrorIs(t, err, client.ErrForbidden)
	})

	t.Run("status code >= 400", func(t *testing.T) {
		t.Run("when body is an error type", func(t *testing.T) {
			err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
				Body:         []byte(`{"message":"failure"}`),
				HTTPResponse: &http.Response{StatusCode: 400},
			})

			require.ErrorContains(t, err, "received response code 400: failure")
		})

		t.Run("when body is not an error type", func(t *testing.T) {
			err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
				Body:         []byte(`unknown error`),
				HTTPResponse: &http.Response{StatusCode: 400},
			})

			require.ErrorContains(t, err, "received response code 400: unknown error")
		})

		t.Run("when body has no message field", func(t *testing.T) {
			err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: 502},
			})

			require.ErrorContains(t, err, "received response code 502 with empty message")
		})
	})

	t.Run("status code < 400", func(t *testing.T) {
		err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
			HTTPResponse: &http.Response{StatusCode: 200},
		})

		require.NoError(t, err)
	})
}

func TestBodyFromResponse(t *testing.T) {
	t.Run("returns the parsed body on success", func(t *testing.T) {
		resp := &client.CreateDeployResponse{
			JSON201:      &client.Deploy{Id: "dep-123456"},
			HTTPResponse: &http.Response{StatusCode: 201},
		}

		body, err := client.BodyFromResponse(resp.JSON201, resp)

		require.NoError(t, err)
		require.Equal(t, "dep-123456", body.Id)
	})

	t.Run("returns the API error for error statuses", func(t *testing.T) {
		resp := &client.CreateDeployResponse{
			Body:         []byte(`{"message":"service not found"}`),
			HTTPResponse: &http.Response{StatusCode: 404},
		}

		_, err := client.BodyFromResponse(resp.JSON201, resp)

		require.ErrorContains(t, err, "received response code 404: service not found")
	})

	t.Run("returns an error for success statuses with no parsed body", func(t *testing.T) {
		resp := &client.CreateDeployResponse{
			HTTPResponse: &http.Response{StatusCode: 202},
		}

		_, err := client.BodyFromResponse(resp.JSON201, resp)

		require.ErrorContains(t, err, "received response code 202 with an unexpected empty body")
	})
}
