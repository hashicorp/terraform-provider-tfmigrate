package net

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHttpClient_DoRequest(t *testing.T) {
	assertions := assert.New(t)

	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Set a custom header for testing
		rw.Header().Set("X-Test-Header", "test-value")

		_, err := rw.Write([]byte(`{"status": "ok"}`))
		if err != nil {
			return
		}
	}))
	// Close the server when the test finishes
	defer server.Close()

	type fields struct {
		client *http.Client
	}
	type args struct {
		method  string
		url     string
		headers map[string]string
		body    io.Reader
	}
	tests := []struct {
		name               string
		fields             fields
		args               args
		expectedResponse   []byte
		expectedStatusCode int
		wantErr            bool
	}{
		{
			name: "Test 1",
			fields: fields{
				client: server.Client(),
			},
			args: args{
				method:  "GET",
				url:     server.URL + "/some/path",
				headers: map[string]string{},
				body:    nil,
			},
			expectedResponse:   []byte(`{"status": "ok"}`),
			expectedStatusCode: 200,
			wantErr:            false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHttpClient(5 * time.Second)
			statusCode, resp, headers, err := hc.DoRequest(context.Background(), tt.args.method, tt.args.url, tt.args.headers, tt.args.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("DoRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assertions.Equal(tt.expectedResponse, resp)
			assertions.Equal(tt.expectedStatusCode, statusCode)
			assertions.NotNil(headers)
			assertions.Equal("test-value", headers.Get("X-Test-Header"))
		})
	}
}
