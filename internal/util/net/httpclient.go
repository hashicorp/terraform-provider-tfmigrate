package net

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

type MethodType string

const (
	defaultTimeout      = 15 * time.Second
	defaultRetryMax     = 3
	defaultRetryWaitMin = 100 * time.Millisecond
	defaultRetryWaitMax = 2 * time.Second

	// HTTP methods.
	MethodGet    MethodType = "GET"
	MethodPost   MethodType = "POST"
	MethodPut    MethodType = "PUT"
	MethodDelete MethodType = "DELETE"
)

var allowedMethods = []string{string(MethodGet), string(MethodPost), string(MethodPut), string(MethodDelete)}

type Client interface {
	Do(ctx context.Context, opts RequestOptions) (*http.Response, error)
	SetTlsConfig(tlsConfig *tls.Config) error
}

type httpClient struct {
	client *retryablehttp.Client
}

type RequestOptions struct {
	Method      MethodType
	URL         string
	Headers     map[string]string
	QueryParams map[string]string
	Body        io.Reader
}

// NewClient creates an HTTP client with safe defaults (via cleanhttp) and custom timeout.
func NewClient() Client {
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = &http.Client{
		Timeout:   defaultTimeout,
		Transport: cleanhttp.DefaultPooledTransport(),
	}
	retryClient.RetryMax = defaultRetryMax
	retryClient.RetryWaitMin = defaultRetryWaitMin
	retryClient.RetryWaitMax = defaultRetryWaitMax
	return &httpClient{
		client: retryClient,
	}
}

func (hc *httpClient) Do(ctx context.Context, opts RequestOptions) (*http.Response, error) {
	// Validate HTTP method
	method := strings.ToUpper(string(opts.Method))
	if !isValidMethod(method) {
		return nil, fmt.Errorf("unsupported HTTP method: %s. Allowed methods: %v", opts.Method, allowedMethods)
	}

	reqURL, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if len(opts.QueryParams) > 0 {
		q := reqURL.Query()
		for k, v := range opts.QueryParams {
			q.Set(k, v)
		}
		reqURL.RawQuery = q.Encode()
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, string(opts.Method), reqURL.String(), opts.Body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making HTTP request: %w", err)
	}

	return resp, nil
}

// SetTlsConfig sets the TLS configuration for the HTTP client.
func (hc *httpClient) SetTlsConfig(tlsConfig *tls.Config) error {
	if tlsConfig == nil {
		return fmt.Errorf("TLS configuration cannot be nil")
	}

	transport := hc.client.HTTPClient.Transport.(*http.Transport)
	transport.TLSClientConfig = tlsConfig
	return nil
}

func isValidMethod(method string) bool {
	for _, allowed := range allowedMethods {
		if method == allowed {
			return true
		}
	}
	return false
}
