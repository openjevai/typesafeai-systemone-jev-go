package jev

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

// RetryPolicy controls how the client retries failed requests.
//
// The zero value disables retrying: it performs one attempt per call. Start
// from DefaultRetryPolicy to customize the SDK defaults:
//
//	policy := jev.DefaultRetryPolicy()
//	policy.MaxRetries = 5
//	client, err := jev.NewClient(jev.WithRetryPolicy(policy))
//
// A RetryPolicy can also be supplied per call with WithCallRetryPolicy.
type RetryPolicy struct {
	// MaxRetries is the number of retries after the initial attempt. Zero
	// disables retries. Default: 2.
	MaxRetries int

	// BackoffInitial is the delay before the first retry; it doubles each
	// attempt up to BackoffMax. Zero disables backoff. Default: 500ms.
	BackoffInitial time.Duration

	// BackoffMax caps the exponential backoff delay. Default: 5s.
	BackoffMax time.Duration

	// BackoffJitter is the fraction of each backoff delay that is randomly
	// subtracted, between 0 and 1, to spread out retries from many clients.
	// Default: 0.25.
	BackoffJitter float64

	// HTTPStatuses lists the HTTP status codes that are retried. When nil it
	// defaults to 408, 429, and every 5xx status. An empty, non-nil slice
	// disables status-based retries.
	HTTPStatuses []int

	// RespectRetryAfter honors the Retry-After and retry-after-ms response
	// headers, waiting at least the requested time before retrying.
	// Default: true.
	RespectRetryAfter bool

	// RetryOnConnectionError retries requests that failed without an HTTP
	// response. Default: true.
	RetryOnConnectionError bool

	// RetryOnTimeout retries requests that exceeded their per-attempt
	// timeout. Default: true.
	RetryOnTimeout bool

	// TotalTimeout is the retry budget for one call, including the initial
	// attempt and all delays. Retries whose delay would exceed the remaining
	// budget are not attempted. Zero disables the budget. Default: 30s.
	TotalTimeout time.Duration

	// Predicate is an optional escape hatch called with each error; returning
	// true retries in addition to the rules above.
	Predicate func(error) bool
}

// DefaultRetryPolicy returns the retry behavior this SDK uses when no policy
// is supplied: two retries with jittered exponential backoff from 500ms to
// 5s, a 30s retry budget, and retries on 408, 429, 5xx, connection errors,
// and timeouts, honoring Retry-After.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:             2,
		BackoffInitial:         500 * time.Millisecond,
		BackoffMax:             5 * time.Second,
		BackoffJitter:          0.25,
		HTTPStatuses:           defaultRetryStatuses(),
		RespectRetryAfter:      true,
		RetryOnConnectionError: true,
		RetryOnTimeout:         true,
		TotalTimeout:           30 * time.Second,
	}
}

// defaultRetryStatuses returns 408, 429, and every 5xx status.
func defaultRetryStatuses() []int {
	statuses := []int{408, 429}
	for status := 500; status < 600; status++ {
		statuses = append(statuses, status)
	}
	return statuses
}

// validate reports invalid policy settings.
func (p RetryPolicy) validate() error {
	if p.MaxRetries < 0 {
		return fmt.Errorf("%w: retry MaxRetries must not be negative", ErrInvalidConfiguration)
	}
	if p.BackoffInitial < 0 || p.BackoffMax < 0 {
		return fmt.Errorf("%w: retry backoff must not be negative", ErrInvalidConfiguration)
	}
	if p.BackoffJitter < 0 || p.BackoffJitter > 1 {
		return fmt.Errorf("%w: retry BackoffJitter must be between 0 and 1", ErrInvalidConfiguration)
	}
	if p.TotalTimeout < 0 {
		return fmt.Errorf("%w: retry TotalTimeout must not be negative", ErrInvalidConfiguration)
	}
	for _, status := range p.HTTPStatuses {
		if status < 100 || status > 599 {
			return fmt.Errorf("%w: retry status %d is not a valid HTTP status code", ErrInvalidConfiguration, status)
		}
	}
	return nil
}

// statuses returns the effective status code list.
func (p RetryPolicy) statuses() []int {
	if p.HTTPStatuses == nil {
		return defaultRetryStatuses()
	}
	return p.HTTPStatuses
}

// shouldRetry reports whether err triggers a retry under this policy.
func (p RetryPolicy) shouldRetry(err error) bool {
	retry := false
	var timeoutErr *TimeoutError
	var connectionErr *ConnectionError
	var apiErr *APIError
	switch {
	case errors.As(err, &timeoutErr):
		retry = p.RetryOnTimeout
	case errors.As(err, &connectionErr):
		retry = p.RetryOnConnectionError
	case errors.As(err, &apiErr):
		retry = slices.Contains(p.statuses(), apiErr.StatusCode)
	}
	if !retry && p.Predicate != nil {
		retry = p.Predicate(err)
	}
	return retry
}

// delay returns how long to wait before the retry identified by attempt
// (1 for the first retry).
func (p RetryPolicy) delay(err error, attempt int) time.Duration {
	if p.RespectRetryAfter {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if delay, ok := parseRetryAfter(apiErr.Header); ok {
				return delay
			}
		}
	}
	return p.backoff(attempt)
}

// backoff computes the jittered exponential delay for an attempt.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	if p.BackoffInitial <= 0 || p.BackoffMax <= 0 {
		return 0
	}
	exponential := float64(p.BackoffInitial) * math.Pow(2, float64(attempt-1))
	if exponential > float64(p.BackoffMax) {
		exponential = float64(p.BackoffMax)
	}
	jitter := 1 - p.BackoffJitter*rand.Float64()
	return time.Duration(exponential * jitter)
}
