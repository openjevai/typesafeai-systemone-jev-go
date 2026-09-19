package jev

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a client pointed at a test server. It clears the
// TYPESAFE_* environment so a developer's real key never leaks into tests.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvBaseURL, "")
	t.Setenv(EnvModel, "")
	t.Setenv(EnvLogLevel, "")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base := []Option{
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithTimeout(2 * time.Second),
		WithRetryPolicy(RetryPolicy{}),
	}
	client, err := NewClient(append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// roundTripFunc adapts a function into an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// jsonResponse builds an HTTP response with a JSON body.
func jsonResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

// mustMap asserts a decoded JSON value is an object.
func mustMap(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value %v (%T) is not an object", value, value)
	}
	return object
}

// mustSlice asserts a decoded JSON value is an array.
func mustSlice(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value %v (%T) is not an array", value, value)
	}
	return array
}

// decodeJSONBody reads and decodes a request body into a generic map.
func decodeJSONBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode request body %q: %v", raw, err)
	}
	return decoded
}
