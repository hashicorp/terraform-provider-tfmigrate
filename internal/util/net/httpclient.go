package net

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

type httpClient struct {
	client *retryablehttp.Client
}

type HttpClient interface {
	DoRequest(ctx context.Context, method, url string, headers map[string]string, body io.Reader) (int, []byte, http.Header, error)
}

func NewHttpClient(timeout time.Duration) HttpClient {
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient.Timeout = timeout
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 5 * time.Second
	retryClient.Logger = nil // Disable retryablehttp logging

	return &httpClient{
		client: retryClient,
	}
}

// DoRequest executes an HTTP request with context, method, URL, headers, and body.
func (hc *httpClient) DoRequest(ctx context.Context, method, url string, headers map[string]string, body io.Reader) (int, []byte, http.Header, error) {
	// Create the request
	req, err := retryablehttp.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers to the request
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Execute the request
	resp, err := hc.client.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, resp.Header, fmt.Errorf("failed to read response body: %w", err)
	}

	return resp.StatusCode, responseBytes, resp.Header, nil
}
