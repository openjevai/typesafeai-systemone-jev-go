package jev

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSystemOneAsync(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul","noul":0.9}},"usage":{"input_tokens":5,"output_tokens":1}}`))
	})

	future := client.SystemOneAsync(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if future == nil {
		t.Fatal("SystemOneAsync returned nil")
	}
	select {
	case <-future.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("future never completed")
	}
	response, err := future.Result()
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if response.Model != "jev-latest" {
		t.Errorf("Model = %q", response.Model)
	}
	answer, ok := response.Noul("q")
	if !ok || answer.Noul != 0.9 {
		t.Errorf("answer = %+v, %v", answer, ok)
	}

	// Result is repeatable and Wait returns the same value.
	if _, err := future.Result(); err != nil {
		t.Errorf("second Result: %v", err)
	}
	if _, err := future.Wait(context.Background()); err != nil {
		t.Errorf("Wait: %v", err)
	}
}

func TestSystemOneAsyncValidationError(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	future := client.SystemOneAsync(context.Background(), SystemOneRequest{State: "hello"})
	if _, err := future.Result(); err == nil {
		t.Fatal("expected a validation error")
	}
	if calls.Load() != 0 {
		t.Errorf("server saw %d calls, want 0", calls.Load())
	}
}

func TestSystemOneAsyncContextCancellation(t *testing.T) {
	release := make(chan struct{})
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	future := client.SystemOneAsync(ctx, SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	cancel()
	if _, err := future.Result(); err == nil {
		t.Fatal("expected a cancellation error")
	}
}

func TestFutureWaitTimeoutDoesNotCancelTheCall(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	started := make(chan struct{})
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	}, WithTimeout(0))
	t.Cleanup(unblock)

	future := client.SystemOneAsync(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	<-started

	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := future.Wait(waitCtx); err == nil {
		t.Fatal("expected a wait timeout")
	}
	// The call itself is still running and completes normally.
	unblock()
	if _, err := future.Result(); err != nil {
		t.Fatalf("Result: %v", err)
	}
}

func TestListModelsAsync(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"jev-latest","description":"d","release_date":"2026-09-15"}]}`))
	})
	models, err := client.ListModelsAsync(context.Background()).Result()
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if len(models.Models) != 1 || models.Models[0].Name != "jev-latest" {
		t.Errorf("models = %+v", models.Models)
	}
}
