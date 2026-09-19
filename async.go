package jev

import "context"

// Future is the deferred result of an asynchronous call started by
// SystemOneAsync or ListModelsAsync.
//
// A Future is safe for concurrent use. The context passed to the call that
// created it controls the call's lifetime; the context passed to Wait only
// controls how long the caller waits. To cancel a call, cancel the context you
// gave SystemOneAsync.
//
//	future := client.SystemOneAsync(ctx, request)
//	// ... do other work ...
//	response, err := future.Result()
type Future[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// newFuture runs fn in a new goroutine and returns a Future for its result.
func newFuture[T any](fn func() (T, error)) *Future[T] {
	future := &Future[T]{done: make(chan struct{})}
	go func() {
		defer close(future.done)
		future.value, future.err = fn()
	}()
	return future
}

// Done returns a channel that is closed when the call finishes. Use it to
// wait for several futures at once with a select.
func (f *Future[T]) Done() <-chan struct{} { return f.done }

// Result waits for the call to finish and returns its result. It may be
// called any number of times and from several goroutines; the result never
// changes.
func (f *Future[T]) Result() (T, error) {
	<-f.done
	return f.value, f.err
}

// Wait waits for the call to finish or for ctx to be done, whichever happens
// first. Cancelling ctx does not cancel the call; cancel the context passed
// to SystemOneAsync instead. On timeout, Wait returns a zero value and
// ctx.Err().
func (f *Future[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-f.done:
		return f.value, f.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// SystemOneAsync starts a SystemOne call in the background and returns a
// Future for its response.
//
// Use it when a request is one step of a larger workflow that can do other
// work while TypeSafe evaluates. To run many requests with bounded
// concurrency, prefer StreamSystemOne, which starts and collects calls for
// you.
func (c *Client) SystemOneAsync(ctx context.Context, req SystemOneRequest, opts ...CallOption) *Future[*SystemOneResponse] {
	return newFuture(func() (*SystemOneResponse, error) {
		return c.SystemOne(ctx, req, opts...)
	})
}

// ListModelsAsync starts a ListModels call in the background and returns a
// Future for its response.
func (c *Client) ListModelsAsync(ctx context.Context, opts ...CallOption) *Future[*ModelsResponse] {
	return newFuture(func() (*ModelsResponse, error) {
		return c.ListModels(ctx, opts...)
	})
}
