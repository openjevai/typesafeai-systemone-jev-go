package jev

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAPIErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{
			name: "full",
			err: &APIError{
				StatusCode: http.StatusUnprocessableEntity,
				Endpoint:   "POST https://api.typesafe.ai/v1/systemone",
				Message:    "questions.urgency: Field required",
				RequestID:  "req_1",
			},
			want: "typesafe: POST https://api.typesafe.ai/v1/systemone: 422 questions.urgency: Field required (request_id=req_1)",
		},
		{
			name: "no message with empty body",
			err:  &APIError{StatusCode: http.StatusBadGateway},
			want: "typesafe: 502 (no body)",
		},
		{
			name: "message from raw body",
			err: &APIError{
				StatusCode: http.StatusBadGateway,
				Message:    "bad gateway",
			},
			want: "typesafe: 502 bad gateway",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Error(); got != test.want {
				t.Errorf("Error() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNewAPIErrorMapping(t *testing.T) {
	header := http.Header{}
	header.Set(headerRequestID, "req_x")
	tests := []struct {
		status  int
		wantAs  func(error) bool
		wantRaw bool // true when the error is the base *APIError
	}{
		{http.StatusBadRequest, func(err error) bool { var target *BadRequestError; return errors.As(err, &target) }, false},
		{http.StatusUnauthorized, func(err error) bool { var target *AuthenticationError; return errors.As(err, &target) }, false},
		{http.StatusForbidden, func(err error) bool { var target *PermissionDeniedError; return errors.As(err, &target) }, false},
		{http.StatusNotFound, func(err error) bool { var target *NotFoundError; return errors.As(err, &target) }, false},
		{http.StatusUnprocessableEntity, func(err error) bool { var target *UnprocessableEntityError; return errors.As(err, &target) }, false},
		{http.StatusTooManyRequests, func(err error) bool { var target *RateLimitError; return errors.As(err, &target) }, false},
		{http.StatusInternalServerError, func(err error) bool { var target *InternalServerError; return errors.As(err, &target) }, false},
		{http.StatusServiceUnavailable, func(err error) bool { var target *InternalServerError; return errors.As(err, &target) }, false},
		{http.StatusTeapot, func(err error) bool { return false }, true},
	}
	for _, test := range tests {
		err := newAPIError(test.status, []byte(`{"message":"m"}`), header, "GET https://example.test")
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: err = %T, want an *APIError", test.status, err)
		}
		if apiErr.StatusCode != test.status {
			t.Errorf("status %d: StatusCode = %d", test.status, apiErr.StatusCode)
		}
		if apiErr.RequestID != "req_x" {
			t.Errorf("status %d: RequestID = %q", test.status, apiErr.RequestID)
		}
		if test.wantAs(err) == test.wantRaw {
			t.Errorf("status %d: error type mismatch (%T)", test.status, err)
		}
	}
}

func TestRateLimitErrorRetryAfter(t *testing.T) {
	header := http.Header{"Retry-After": []string{"3"}}
	err := newAPIError(http.StatusTooManyRequests, nil, header, "")
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("err = %T, want *RateLimitError", err)
	}
	if rateLimit.RetryAfter != 3*time.Second {
		t.Errorf("RetryAfter = %v, want 3s", rateLimit.RetryAfter)
	}
}

func TestErrorWrappingChains(t *testing.T) {
	connection := &ConnectionError{Err: errors.New("dial tcp: refused")}
	if !strings.Contains(connection.Error(), "refused") {
		t.Errorf("Error() = %q", connection.Error())
	}
	if connection.Unwrap() == nil {
		t.Error("ConnectionError should unwrap to its cause")
	}

	timeout := &TimeoutError{Timeout: time.Second, Err: errors.New("i/o timeout")}
	if !errors.Is(timeout, ErrTimeout) {
		t.Error("TimeoutError should match ErrTimeout")
	}
	if timeout.Unwrap() == nil {
		t.Error("TimeoutError should unwrap to its cause")
	}
	if !strings.Contains(timeout.Error(), "1s") {
		t.Errorf("Error() = %q", timeout.Error())
	}

	empty := &TimeoutError{}
	if !strings.Contains(empty.Error(), "timed out") {
		t.Errorf("Error() = %q", empty.Error())
	}
}

func TestInvalidResponseErrorMessage(t *testing.T) {
	err := &InvalidResponseError{
		StatusCode: http.StatusOK,
		Body:       []byte("not json"),
		Err:        errors.New("invalid character 'o'"),
	}
	if !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("Error() = %q", err.Error())
	}
	if err.Unwrap() == nil {
		t.Error("InvalidResponseError should unwrap to its cause")
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"plain text", "gateway timeout", "gateway timeout"},
		{"json string", `"quoted error"`, `"quoted error"`},
		{"error string", `{"error":"boom"}`, "boom"},
		{"error object", `{"error":{"message":"boom"}}`, "boom"},
		{"message", `{"message":"boom"}`, "boom"},
		{"detail string", `{"detail":"boom"}`, "boom"},
		{"detail object", `{"detail":{"message":"boom"}}`, "boom"},
		{
			"validation list",
			`{"detail":[{"loc":["body","questions","urgency"],"msg":"Field required"},{"loc":["body","state"],"msg":"Field required"}]}`,
			"questions.urgency: Field required; state: Field required",
		},
		{"validation list without location", `{"detail":[{"msg":"Field required"}]}`, "Field required"},
		{"empty", ``, ""},
		{"unrelated json", `{"other":1}`, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractErrorMessage([]byte(test.body)); got != test.want {
				t.Errorf("extractErrorMessage(%q) = %q, want %q", test.body, got, test.want)
			}
		})
	}
}

func TestFormatValidationPath(t *testing.T) {
	if got := formatValidationPath([]any{"body", "questions", "log", float64(2)}); got != "questions.log.2" {
		t.Errorf("path = %q", got)
	}
	if got := formatValidationPath("not-a-list"); got != "" {
		t.Errorf("path = %q, want empty", got)
	}
	if got := formatValidationPath(nil); got != "" {
		t.Errorf("path = %q, want empty", got)
	}
}
