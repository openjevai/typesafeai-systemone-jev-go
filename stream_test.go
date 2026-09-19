package jev

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// streamRequests builds n requests whose state encodes the request index.
func streamRequests(n int) []SystemOneRequest {
	requests := make([]SystemOneRequest, n)
	for index := range requests {
		requests[index] = SystemOneRequest{
			State:     fmt.Sprintf("request-%d", index),
			Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
		}
	}
	return requests
}

// streamIndex reads the request index encoded in the state.
func streamIndex(t *testing.T, r *http.Request) int {
	t.Helper()
	body := decodeJSONBody(t, r)
	state, _ := body["state"].(string)
	index, err := strconv.Atoi(strings.TrimPrefix(state, "request-"))
	if err != nil {
		t.Fatalf("state %q: %v", state, err)
	}
	return index
}

func TestStreamSystemOneUnordered(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		index := streamIndex(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"model":"model-%d","answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`, index)
	})

	results, err := client.StreamSystemOne(context.Background(), streamRequests(7), WithStreamConcurrency(3))
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	seen := map[int]bool{}
	for result := range results {
		if result.Err != nil {
			t.Fatalf("request %d: %v", result.Index, result.Err)
		}
		seen[result.Index] = true
		if want := fmt.Sprintf("model-%d", result.Index); result.Response.Model != want {
			t.Errorf("request %d: model = %q, want %q", result.Index, result.Response.Model, want)
		}
	}
	if len(seen) != 7 {
		t.Errorf("saw %d results, want 7", len(seen))
	}
}

func TestStreamSystemOneOrdered(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		index := streamIndex(t, r)
		// Later requests finish first, so ordered emission must hold them back.
		time.Sleep(time.Duration(6-index) * 5 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"model":"model-%d","answers":{},"usage":{}}`, index)
	})

	results, err := client.StreamSystemOne(context.Background(), streamRequests(6),
		WithStreamConcurrency(6), WithStreamOrdered(true))
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	previous := -1
	for result := range results {
		if result.Err != nil {
			t.Fatalf("request %d: %v", result.Index, result.Err)
		}
		if result.Index <= previous {
			t.Fatalf("result %d arrived after %d, want input order", result.Index, previous)
		}
		previous = result.Index
	}
	if previous != 5 {
		t.Errorf("last index = %d, want 5", previous)
	}
}

func TestStreamSystemOneRespectsConcurrency(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			observed := maxInFlight.Load()
			if current <= observed || maxInFlight.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	results, err := client.StreamSystemOne(context.Background(), streamRequests(9), WithStreamConcurrency(3))
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	count := 0
	for range results {
		count++
	}
	if count != 9 {
		t.Errorf("results = %d, want 9", count)
	}
	if maxInFlight.Load() > 3 {
		t.Errorf("max in flight = %d, want at most 3", maxInFlight.Load())
	}
	if maxInFlight.Load() < 2 {
		t.Errorf("max in flight = %d, want the pool to run concurrently", maxInFlight.Load())
	}
}

func TestStreamSystemOneCallOptions(t *testing.T) {
	var sawHeader atomic.Bool
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Stream") == "yes" {
			sawHeader.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	results, err := client.StreamSystemOne(context.Background(), streamRequests(2),
		WithStreamCallOptions(WithCallHeader("X-Stream", "yes")))
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	for range results {
	}
	if !sawHeader.Load() {
		t.Error("stream call options were not applied")
	}
}

func TestStreamSystemOnePerRequestErrors(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	requests := streamRequests(3)
	requests[1] = SystemOneRequest{Questions: Questions{"q": Noul{Instructions: "Is this hello?"}}} // no state
	results, err := client.StreamSystemOne(context.Background(), requests)
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	byIndex := map[int]StreamResult{}
	for result := range results {
		byIndex[result.Index] = result
	}
	if len(byIndex) != 3 {
		t.Fatalf("results = %d, want 3", len(byIndex))
	}
	if byIndex[1].Err == nil || !strings.Contains(byIndex[1].Err.Error(), "state") {
		t.Errorf("index 1 error = %v, want a state validation error", byIndex[1].Err)
	}
	if byIndex[0].Err != nil || byIndex[2].Err != nil {
		t.Errorf("valid requests failed: %v, %v", byIndex[0].Err, byIndex[2].Err)
	}
	if calls.Load() != 2 {
		t.Errorf("server saw %d calls, want 2", calls.Load())
	}
}

func TestStreamSystemOneRejectsInvalidOptions(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := client.StreamSystemOne(context.Background(), streamRequests(1), WithStreamConcurrency(0)); err == nil {
		t.Fatal("expected an error for concurrency 0")
	}
}

func TestStreamSystemOneEmpty(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	results, err := client.StreamSystemOne(context.Background(), nil)
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	count := 0
	for range results {
		count++
	}
	if count != 0 {
		t.Errorf("results = %d, want 0", count)
	}
}

func TestStreamSystemOneCancellationClosesTheChannel(t *testing.T) {
	release := make(chan struct{})
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	results, err := client.StreamSystemOne(ctx, streamRequests(10), WithStreamConcurrency(2))
	if err != nil {
		t.Fatalf("StreamSystemOne: %v", err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range results {
		}
	}()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not close after cancellation")
	}
}
