package net_mocks

import (
	"net/http"

	"github.com/stretchr/testify/mock"
)

// MockRoundTripper is a mock implementation of the http.RoundTripper interface.
type MockRoundTripper struct {
	mock.Mock
}

// RoundTrip mocks the RoundTrip method of the http.RoundTripper interface.
func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	//handle the case where the first argument is nil
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*http.Response), args.Error(1)
}
