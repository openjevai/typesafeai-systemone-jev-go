package jev

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", policy.MaxRetries)
	}
	if policy.BackoffInitial != 500*time.Millisecond {
		t.Errorf("BackoffInitial = %v", policy.BackoffInitial)
	}
	if policy.BackoffMax != 5*time.Second {
		t.Errorf("BackoffMax = %v", policy.BackoffMax)
	}
	if policy.BackoffJitter != 0.25 {
		t.Errorf("BackoffJitter = %v", policy.BackoffJitter)
	}
	if !policy.RespectRetryAfter || !policy.RetryOnConnectionError || !policy.RetryOnTimeout {
		t.Errorf("default policy flags = %+v", policy)
	}
	if policy.TotalTimeout != 30*time.Second {
		t.Errorf("TotalTimeout = %v", policy.TotalTimeout)
	}
	if len(policy.statuses()) == 0 {
		t.Error("default statuses should not be empty")
	}
}

func TestRetryOnRateLimit(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"slow down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	}, WithRetryPolicy(RetryPolicy{
		MaxRetries:             1,
		BackoffInitial:         time.Millisecond,
		BackoffMax:             time.Millisecond,
		RespectRetryAfter:      true,
		RetryOnConnectionError: true,
		HTTPStatuses:           defaultRetryStatuses(),
	}))

	if _, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestRetryExhaustionReturnsRateLimitError(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow down"}`))
	}, WithRetryPolicy(RetryPolicy{
		MaxRetries:             2,
		BackoffInitial:         time.Millisecond,
		BackoffMax:             time.Millisecond,
		RespectRetryAfter:      true,
		RetryOnConnectionError: true,
		HTTPStatuses:           defaultRetryStatuses(),
	}))

	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("err = %v, want *RateLimitError", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
	if rateLimit.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0", rateLimit.RetryAfter)
	}
}

func TestNoRetryOnClientErrors(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusUnprocessableEntity,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"nope"}`))
			}, WithRetryPolicy(DefaultRetryPolicy()))

			_, err := client.SystemOne(context.Background(), SystemOneRequest{
				State:     "hello",
				Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			if attempts.Load() != 1 {
				t.Errorf("attempts = %d, want 1", attempts.Load())
			}
		})
	}
}

func TestRetriesDisabledByZeroPolicy(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	})

	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1", attempts.Load())
	}
}

func TestRetryOnConnectionError(t *testing.T) {
	var attempts atomic.Int32
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.test.invalid"),
		WithTimeout(time.Second),
		WithRetryPolicy(RetryPolicy{
			MaxRetries:             3,
			BackoffInitial:         time.Millisecond,
			BackoffMax:             time.Millisecond,
			RetryOnConnectionError: true,
		}),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if attempts.Add(1) < 3 {
				return nil, errors.New("connection reset")
			}
			return jsonResponse(request, http.StatusOK, minimalResponse), nil
		})}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
}

func TestConnectionErrorIsReportedWhenRetriesAreDisabled(t *testing.T) {
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.test.invalid"),
		WithRetryPolicy(RetryPolicy{}),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		})}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	var connectionErr *ConnectionError
	if !errors.As(err, &connectionErr) {
		t.Fatalf("err = %v, want *ConnectionError", err)
	}
}

func TestRetryOnTimeout(t *testing.T) {
	var attempts atomic.Int32
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.test.invalid"),
		WithTimeout(10*time.Millisecond),
		WithRetryPolicy(RetryPolicy{
			MaxRetries:     2,
			BackoffInitial: time.Millisecond,
			BackoffMax:     time.Millisecond,
			RetryOnTimeout: true,
		}),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts.Add(1)
			select {
			case <-time.After(200 * time.Millisecond):
				return jsonResponse(request, http.StatusOK, minimalResponse), nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		})}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("err = %v, want *TimeoutError", err)
	}
	if timeoutErr.Timeout != 10*time.Millisecond {
		t.Errorf("Timeout = %v, want 10ms", timeoutErr.Timeout)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
}

func TestTimeoutNotRetriedWhenDisabled(t *testing.T) {
	var attempts atomic.Int32
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.test.invalid"),
		WithTimeout(10*time.Millisecond),
		WithRetryPolicy(RetryPolicy{MaxRetries: 3}),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts.Add(1)
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1", attempts.Load())
	}
}

func TestRetryBudgetStopsRetrying(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}, WithRetryPolicy(RetryPolicy{
		MaxRetries:     5,
		BackoffInitial: time.Second,
		BackoffMax:     time.Second,
		TotalTimeout:   10 * time.Millisecond,
		HTTPStatuses:   defaultRetryStatuses(),
	}))

	start := time.Now()
	if _, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	}); err == nil {
		t.Fatal("expected an error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 before the budget stop", attempts.Load())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("call took %v, want the budget to stop the retry", elapsed)
	}
}

func TestRetryStopsWhenContextIsCanceledDuringBackoff(t *testing.T) {
	var attempts atomic.Int32
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.test.invalid"),
		WithTimeout(0),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return jsonResponse(request, http.StatusInternalServerError, `{"message":"boom"}`), nil
		})}),
		WithRetryPolicy(RetryPolicy{
			MaxRetries:     3,
			BackoffInitial: time.Minute,
			BackoffMax:     time.Minute,
			TotalTimeout:   0,
			HTTPStatuses:   defaultRetryStatuses(),
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.SystemOne(ctx, SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1", attempts.Load())
	}
}

func TestRetryPredicate(t *testing.T) {
	var attempts atomic.Int32
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.test.invalid"),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if attempts.Add(1) == 1 {
				// A 404 is not normally retried.
				return jsonResponse(request, http.StatusNotFound, `{"message":"not yet"}`), nil
			}
			return jsonResponse(request, http.StatusOK, minimalResponse), nil
		})}),
		WithRetryPolicy(RetryPolicy{
			MaxRetries: 1,
			Predicate: func(err error) bool {
				var notFound *NotFoundError
				return errors.As(err, &notFound)
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestBackoffWithinJitterBounds(t *testing.T) {
	policy := RetryPolicy{
		BackoffInitial: 100 * time.Millisecond,
		BackoffMax:     400 * time.Millisecond,
		BackoffJitter:  0.5,
	}
	want := map[int][2]time.Duration{
		1: {50 * time.Millisecond, 100 * time.Millisecond},
		2: {100 * time.Millisecond, 200 * time.Millisecond},
		3: {200 * time.Millisecond, 400 * time.Millisecond},
		4: {200 * time.Millisecond, 400 * time.Millisecond},
	}
	for attempt, bounds := range want {
		for iteration := 0; iteration < 50; iteration++ {
			delay := policy.backoff(attempt)
			if delay < bounds[0] || delay > bounds[1] {
				t.Fatalf("backoff(%d) = %v, want within [%v, %v]", attempt, delay, bounds[0], bounds[1])
			}
		}
	}
}

func TestBackoffDisabled(t *testing.T) {
	policy := RetryPolicy{BackoffInitial: 0, BackoffMax: time.Second}
	if delay := policy.backoff(1); delay != 0 {
		t.Errorf("backoff = %v, want 0", delay)
	}
}

func TestRetryDelayPrefersRetryAfterHeader(t *testing.T) {
	policy := DefaultRetryPolicy()
	header := http.Header{}
	header.Set("Retry-After", "2.5")
	apiErr := &RateLimitError{APIError: &APIError{StatusCode: http.StatusTooManyRequests, Header: header}}
	if delay := policy.delay(apiErr, 1); delay != 2500*time.Millisecond {
		t.Errorf("delay = %v, want 2.5s", delay)
	}
	if delay := policy.delay(&APIError{StatusCode: http.StatusBadRequest}, 1); delay >= time.Second {
		t.Errorf("delay = %v, want the default backoff", delay)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
		ok     bool
	}{
		{"milliseconds", http.Header{"Retry-After-Ms": []string{"1500"}}, 1500 * time.Millisecond, true},
		{"seconds", http.Header{"Retry-After": []string{"2"}}, 2 * time.Second, true},
		{"fractional seconds", http.Header{"Retry-After": []string{"0.5"}}, 500 * time.Millisecond, true},
		{"milliseconds win", http.Header{"Retry-After-Ms": []string{"10"}, "Retry-After": []string{"5"}}, 10 * time.Millisecond, true},
		{"http date in the past", http.Header{"Retry-After": []string{"Mon, 02 Jan 2006 15:04:05 GMT"}}, 0, true},
		{"negative seconds", http.Header{"Retry-After": []string{"-1"}}, 0, false},
		{"garbage", http.Header{"Retry-After": []string{"soon"}}, 0, false},
		{"absent", http.Header{}, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delay, ok := parseRetryAfter(test.header)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if ok && delay != test.want {
				t.Errorf("delay = %v, want %v", delay, test.want)
			}
		})
	}
}

func TestBackoffNeverExceedsMax(t *testing.T) {
	policy := RetryPolicy{BackoffInitial: time.Second, BackoffMax: 3 * time.Second, BackoffJitter: 0.9}
	for attempt := 1; attempt <= 10; attempt++ {
		exponential := math.Min(float64(time.Second)*math.Pow(2, float64(attempt-1)), float64(3*time.Second))
		delay := policy.backoff(attempt)
		if delay > 3*time.Second {
			t.Fatalf("backoff(%d) = %v, exceeds max", attempt, delay)
		}
		if delay < time.Duration(exponential*0.1) {
			t.Fatalf("backoff(%d) = %v, below jitter floor %v", attempt, delay, time.Duration(exponential*0.1))
		}
	}
}
