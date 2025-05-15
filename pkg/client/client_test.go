package client_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/render-oss/cli/pkg/client"
)

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
	})

	t.Run("status code < 400", func(t *testing.T) {
		err := client.ErrorFromResponse(&client.ListSnapshotsResponse{
			HTTPResponse: &http.Response{StatusCode: 200},
		})

		require.NoError(t, err)
	})
}
