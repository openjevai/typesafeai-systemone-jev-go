package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors. Use errors.Is to test for them.
var (
	// ErrNoAPIKey is returned by NewClient when no API key was supplied and
	// the TYPESAFE_API_KEY environment variable is empty.
	ErrNoAPIKey = errors.New("no API key")

	// ErrInvalidConfiguration is returned for client options that cannot be
	// used to build a working client.
	ErrInvalidConfiguration = errors.New("invalid configuration")

	// ErrInvalidRequest is returned before a request is sent when the request
	// is missing required data or contains a malformed question.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrTimeout reports that a request attempt exceeded its per-attempt
	// timeout. Inspect errors.Is(err, ErrTimeout), or use errors.As with
	// *TimeoutError for the configured duration.
	ErrTimeout = errors.New("request timed out")
)

// ConnectionError reports a request that failed without receiving an HTTP
// response, for example a refused connection or a reset stream.
type ConnectionError struct {
	// Err is the underlying transport error.
	Err error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("typesafe: connection error: %v", e.Err)
}

// Unwrap returns the underlying transport error.
func (e *ConnectionError) Unwrap() error { return e.Err }

// TimeoutError reports a request attempt that exceeded its timeout.
type TimeoutError struct {
	// Timeout is the per-attempt timeout that was exceeded. It is zero when
	// the deadline came from the context rather than the client.
	Timeout time.Duration
	// Err is the underlying error, usually context.DeadlineExceeded.
	Err error
}

func (e *TimeoutError) Error() string {
	if e.Timeout > 0 {
		return fmt.Sprintf("typesafe: request timed out after %s", e.Timeout)
	}
	return "typesafe: request timed out"
}

// Unwrap returns the underlying error.
func (e *TimeoutError) Unwrap() error { return e.Err }

// Is reports whether target is ErrTimeout.
func (e *TimeoutError) Is(target error) bool { return target == ErrTimeout }

// APIError is an unsuccessful HTTP response: any response with a status code
// outside the 2xx range, including 4xx validation failures and 5xx server
// errors. Use errors.As with the more specific types (AuthenticationError,
// RateLimitError, UnprocessableEntityError, ...) or with *APIError to catch
// all of them.
type APIError struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// Endpoint is the request method and URL, without credentials or query
	// parameters.
	Endpoint string
	// Message is a human-readable detail extracted from the response body,
	// when the body carried one.
	Message string
	// Body is the raw response body.
	Body []byte
	// Header holds the response headers.
	Header http.Header
	// RequestID is the x-typesafe-request-id response header, if present.
	// Include it when contacting TypeSafe support about a failure.
	RequestID string
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("typesafe: ")
	if e.Endpoint != "" {
		b.WriteString(e.Endpoint)
		b.WriteString(": ")
	}
	b.WriteString(strconv.Itoa(e.StatusCode))
	if e.Message != "" {
		b.WriteString(" ")
		b.WriteString(e.Message)
	} else if len(e.Body) == 0 {
		b.WriteString(" (no body)")
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request_id=%s)", e.RequestID)
	}
	return b.String()
}

// BadRequestError is a 400 response: the request was malformed.
type BadRequestError struct{ *APIError }

// AuthenticationError is a 401 response: the API key is missing or invalid.
type AuthenticationError struct{ *APIError }

// PermissionDeniedError is a 403 response: the API key cannot access the
// requested resource.
type PermissionDeniedError struct{ *APIError }

// NotFoundError is a 404 response.
type NotFoundError struct{ *APIError }

// UnprocessableEntityError is a 422 response: the request body failed
// validation. The response body usually names the offending field.
type UnprocessableEntityError struct{ *APIError }

// RateLimitError is a 429 response: a rate limit was exceeded. RetryAfter
// carries the server's requested wait when the response included a
// Retry-After or retry-after-ms header. The client already retries 429
// responses automatically according to its RetryPolicy.
type RateLimitError struct {
	*APIError
	// RetryAfter is the server's requested wait; zero when unavailable.
	RetryAfter time.Duration
}

// InternalServerError is a 5xx response: TypeSafe failed to process the
// request. The client already retries these automatically according to its
// RetryPolicy.
type InternalServerError struct{ *APIError }

// Unwrap exposes the shared *APIError so errors.As works for every concrete
// error type above.
func (e *BadRequestError) Unwrap() error          { return e.APIError }
func (e *AuthenticationError) Unwrap() error      { return e.APIError }
func (e *PermissionDeniedError) Unwrap() error    { return e.APIError }
func (e *NotFoundError) Unwrap() error            { return e.APIError }
func (e *UnprocessableEntityError) Unwrap() error { return e.APIError }
func (e *RateLimitError) Unwrap() error           { return e.APIError }
func (e *InternalServerError) Unwrap() error      { return e.APIError }

// InvalidResponseError reports a successful HTTP response whose body could
// not be decoded into the expected shape. This usually means a proxy or a
// newer API version returned something this SDK does not understand.
type InvalidResponseError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Body is the raw response body.
	Body []byte
	// Err is the underlying decoding error.
	Err error
}

func (e *InvalidResponseError) Error() string {
	return fmt.Sprintf("typesafe: invalid response body (status %d): %v", e.StatusCode, e.Err)
}

// Unwrap returns the underlying decoding error.
func (e *InvalidResponseError) Unwrap() error { return e.Err }

// newAPIError builds the most specific error type for an HTTP status code.
func newAPIError(status int, body []byte, header http.Header, endpoint string) error {
	base := &APIError{
		StatusCode: status,
		Endpoint:   endpoint,
		Message:    extractErrorMessage(body),
		Body:       body,
		Header:     header,
		RequestID:  header.Get(headerRequestID),
	}
	switch status {
	case http.StatusBadRequest:
		return &BadRequestError{base}
	case http.StatusUnauthorized:
		return &AuthenticationError{base}
	case http.StatusForbidden:
		return &PermissionDeniedError{base}
	case http.StatusNotFound:
		return &NotFoundError{base}
	case http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{base}
	case http.StatusTooManyRequests:
		retryAfter, _ := parseRetryAfter(header)
		return &RateLimitError{APIError: base, RetryAfter: retryAfter}
	default:
		if status >= 500 {
			return &InternalServerError{base}
		}
		return base
	}
}

// extractErrorMessage pulls a human-readable message out of an error body,
// accepting the shapes returned by the TypeSafe API and its proxies: a plain
// string, {"error": "..."} or {"error": {"message": ...}}, {"message": ...},
// {"detail": "..."}, and FastAPI's {"detail": [{"loc": ..., "msg": ...}]}.
func extractErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	if trimmed[0] != '{' {
		return trimmed
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return trimmed
	}
	if errorValue, ok := decoded["error"]; ok {
		if text, ok := errorValue.(string); ok && text != "" {
			return text
		}
		if object, ok := errorValue.(map[string]any); ok {
			if text, ok := object["message"].(string); ok && text != "" {
				return text
			}
		}
	}
	if text, ok := decoded["message"].(string); ok && text != "" {
		return text
	}
	switch detail := decoded["detail"].(type) {
	case string:
		if detail != "" {
			return detail
		}
	case map[string]any:
		if text, ok := detail["message"].(string); ok && text != "" {
			return text
		}
	case []any:
		parts := make([]string, 0, len(detail))
		for _, entry := range detail {
			item, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			message, ok := item["msg"].(string)
			if !ok {
				continue
			}
			path := formatValidationPath(item["loc"])
			if path == "" {
				parts = append(parts, message)
			} else {
				parts = append(parts, path+": "+message)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return ""
}

// formatValidationPath renders a FastAPI validation location such as
// ["body", "questions", "urgency"] as "questions.urgency".
func formatValidationPath(location any) string {
	items, ok := location.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		text := ""
		switch value := item.(type) {
		case string:
			text = value
		case float64:
			text = strconv.Itoa(int(value))
		}
		if text == "" || text == "body" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ".")
}

// parseRetryAfter reads retry-after-ms (milliseconds, preferred when present)
// and retry-after (seconds, or an HTTP date) from a response.
func parseRetryAfter(header http.Header) (time.Duration, bool) {
	if raw := strings.TrimSpace(header.Get("retry-after-ms")); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value >= 0 {
			return time.Duration(value * float64(time.Millisecond)), true
		}
	}
	raw := strings.TrimSpace(header.Get("retry-after"))
	if raw == "" {
		return 0, false
	}
	if value, err := strconv.ParseFloat(raw, 64); err == nil {
		if value >= 0 {
			return time.Duration(value * float64(time.Second)), true
		}
		return 0, false
	}
	if when, err := http.ParseTime(raw); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}
