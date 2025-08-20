package net

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpClient_Do(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/error" {
			http.Error(rw, "forced error", http.StatusBadRequest)
			return
		}
		_, _ = rw.Write([]byte(`{"status":"ok"}`))
	}))
	defer testServer.Close()

	tests := []struct {
		name               string
		opts               RequestOptions
		expectedStatusCode int
		expectedBody       string
		expectError        bool
	}{
		{
			name: "GET success",
			opts: RequestOptions{
				Method:  http.MethodGet,
				URL:     testServer.URL + "/ok",
				Headers: map[string]string{"Accept": "application/json"},
			},
			expectedStatusCode: 200,
			expectedBody:       `{"status":"ok"}`,
			expectError:        false,
		},
		{
			name: "POST with body",
			opts: RequestOptions{
				Method:  http.MethodPost,
				URL:     testServer.URL + "/ok",
				Headers: map[string]string{"Content-Type": "application/json"},
				Body:    strings.NewReader(`{"foo":"bar"}`),
			},
			expectedStatusCode: 200,
			expectedBody:       `{"status":"ok"}`,
			expectError:        false,
		},
		{
			name: "Bad request (400)",
			opts: RequestOptions{
				Method: http.MethodGet,
				URL:    testServer.URL + "/error",
			},
			expectedStatusCode: 400,
			expectedBody:       "forced error\n",
			expectError:        false,
		},
		{
			name: "Invalid URL",
			opts: RequestOptions{
				Method: http.MethodGet,
				URL:    "://invalid-url",
			},
			expectedStatusCode: 0,
			expectError:        true,
		},
	}

	client := NewClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Do(context.Background(), tt.opts)

			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatusCode, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedBody, string(body))
		})
	}
}
