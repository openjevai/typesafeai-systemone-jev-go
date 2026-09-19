package jev

import (
	"context"
	"fmt"
	"sync"
)

// StreamResult is one completed call from StreamSystemOne.
type StreamResult struct {
	// Index is the position of the request in the slice passed to
	// StreamSystemOne.
	Index int
	// Response holds the result when Err is nil.
	Response *SystemOneResponse
	// Err holds the call's error when the call failed.
	Err error
}

// defaultStreamConcurrency is used when WithStreamConcurrency is not supplied.
const defaultStreamConcurrency = 4

// StreamOption configures StreamSystemOne.
type StreamOption func(*streamConfig) error

// streamConfig collects streaming settings.
type streamConfig struct {
	concurrency int
	ordered     bool
	callOptions []CallOption
}

// WithStreamConcurrency sets how many calls StreamSystemOne keeps in flight.
// The default is 4. TypeSafe rate limits requests per second and tokens per
// second; raise this only as far as the account's limits allow.
func WithStreamConcurrency(n int) StreamOption {
	return func(cfg *streamConfig) error {
		if n < 1 {
			return fmt.Errorf("%w: stream concurrency must be at least 1", ErrInvalidConfiguration)
		}
		cfg.concurrency = n
		return nil
	}
}

// WithStreamOrdered controls emission order. When true, results are emitted
// in input order, which delays later results until every earlier call has
// finished. When false (the default), results are emitted as they complete.
func WithStreamOrdered(ordered bool) StreamOption {
	return func(cfg *streamConfig) error {
		cfg.ordered = ordered
		return nil
	}
}

// WithStreamCallOptions applies call options, such as WithCallTimeout or
// WithCallRetryPolicy, to every request in the stream.
func WithStreamCallOptions(opts ...CallOption) StreamOption {
	return func(cfg *streamConfig) error {
		cfg.callOptions = append(cfg.callOptions, opts...)
		return nil
	}
}

// StreamSystemOne evaluates many independent requests and delivers each
// result on the returned channel as soon as its call finishes. It is the
// batch counterpart to SystemOneAsync: a bounded worker pool runs up to
// WithStreamConcurrency calls at once, and the channel closes when every call
// has finished.
//
//	results, err := client.StreamSystemOne(ctx, requests, jev.WithStreamConcurrency(8))
//	if err != nil {
//		return err
//	}
//	for result := range results {
//		if result.Err != nil {
//			log.Printf("request %d failed: %v", result.Index, result.Err)
//			continue
//		}
//		fmt.Println(result.Index, result.Response.Model)
//	}
//
// Cancelling ctx fails in-flight calls with ctx.Err() and stops the stream;
// cancel it if you stop reading before the channel closes. TypeSafe evaluates
// every question of a request in one response, so there is no partial
// response to stream within a single call: streaming happens across calls.
//
// A request that fails validation is reported as a StreamResult with
// ErrInvalidRequest instead of failing the whole stream.
func (c *Client) StreamSystemOne(ctx context.Context, requests []SystemOneRequest, opts ...StreamOption) (<-chan StreamResult, error) {
	cfg := streamConfig{concurrency: defaultStreamConcurrency}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.concurrency > len(requests) && len(requests) > 0 {
		cfg.concurrency = len(requests)
	}
	results := make(chan StreamResult, max(cfg.concurrency, 1))
	if len(requests) == 0 {
		close(results)
		return results, nil
	}
	if cfg.ordered {
		go c.streamOrdered(ctx, requests, cfg, results)
	} else {
		go c.streamUnordered(ctx, requests, cfg, results)
	}
	return results, nil
}

// streamUnordered runs the worker pool and emits results as they complete.
func (c *Client) streamUnordered(ctx context.Context, requests []SystemOneRequest, cfg streamConfig, out chan<- StreamResult) {
	defer close(out)
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range cfg.concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				response, err := c.SystemOne(ctx, requests[index], cfg.callOptions...)
				select {
				case out <- StreamResult{Index: index, Response: response, Err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	for index := range requests {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

// streamOrdered runs the worker pool and emits results in input order.
func (c *Client) streamOrdered(ctx context.Context, requests []SystemOneRequest, cfg streamConfig, out chan<- StreamResult) {
	defer close(out)
	slots := make([]StreamResult, len(requests))
	done := make([]chan struct{}, len(requests))
	for index := range done {
		done[index] = make(chan struct{})
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range cfg.concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				response, err := c.SystemOne(ctx, requests[index], cfg.callOptions...)
				slots[index] = StreamResult{Index: index, Response: response, Err: err}
				close(done[index])
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range requests {
			jobs <- index
		}
	}()
	for index := range requests {
		<-done[index]
		select {
		case out <- slots[index]:
		case <-ctx.Done():
			// The caller stopped reading; the workers finish on their own
			// because they never exit early.
			return
		}
	}
	workers.Wait()
}
