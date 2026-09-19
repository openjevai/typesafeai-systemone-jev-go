package jev

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// API paths.
const (
	pathSystemOne = "/v1/systemone"
	pathModels    = "/v1/models"
)

// Header names. The SDK identifies itself to TypeSafe the same way the
// official SDKs do, including the retry count on retried attempts.
const (
	headerAuthorization = "Authorization"
	headerAccept        = "Accept"
	headerContentType   = "Content-Type"
	headerUserAgent     = "User-Agent"
	headerSDK           = "X-TypeSafe-SDK"
	headerRuntime       = "X-TypeSafe-Runtime"
	headerRetryCount    = "X-TypeSafe-Retry-Count"
	headerRequestID     = "X-TypeSafe-Request-Id"
	contentTypeJSON     = "application/json"
)

// maxResponseBodyBytes bounds how much of a response body is read into memory.
const maxResponseBodyBytes = 32 << 20 // 32 MiB

// do sends one request, retrying per the effective retry policy, and returns
// the final response with its body already read.
func (c *Client) do(ctx context.Context, method, path string, body []byte, cc callConfig) (*http.Response, []byte, error) {
	timeout := c.timeout
	if cc.timeoutSet {
		timeout = cc.timeout
	}
	policy := c.retry
	if cc.retrySet {
		policy = cc.retry
	}
	endpoint := c.baseURL + path

	header := c.header.Clone()
	for key, values := range cc.header {
		for _, value := range values {
			header.Add(key, value)
		}
	}
	header.Set(headerAuthorization, "Bearer "+c.apiKey)
	header.Set(headerAccept, contentTypeJSON)
	header.Set(headerUserAgent, sdkName+"/"+Version)
	header.Set(headerSDK, sdkName+"/"+Version)
	header.Set(headerRuntime, runtimeInfo())
	if body != nil {
		header.Set(headerContentType, contentTypeJSON)
	}

	start := time.Now()
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt == 0 {
			header.Del(headerRetryCount)
		} else {
			header.Set(headerRetryCount, strconv.Itoa(attempt))
		}

		attemptCtx := ctx
		cancel := context.CancelFunc(func() {})
		if timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		request, err := http.NewRequestWithContext(attemptCtx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, nil, fmt.Errorf("%w: could not build request: %v", ErrInvalidRequest, err)
		}
		request.Header = header.Clone()

		c.logger.Debug("typesafe request", "method", method, "url", endpoint, "attempt", attempt+1)
		attemptStart := time.Now()
		response, err := c.httpClient.Do(request)
		if err != nil {
			cancel()
			// A cancelled or expired parent context means the caller gave
			// up; report that instead of the transport error.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, ctxErr
			}
			lastErr = transportError(err, timeout)
			c.logger.Debug("typesafe transport error", "method", method, "url", endpoint, "error", err)
		} else {
			responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes))
			_ = response.Body.Close()
			cancel()
			c.logger.Debug("typesafe response",
				"method", method,
				"url", endpoint,
				"status", response.StatusCode,
				"duration", time.Since(attemptStart).Round(time.Millisecond),
				"request_id", response.Header.Get(headerRequestID),
			)
			switch {
			case readErr != nil:
				lastErr = &ConnectionError{Err: readErr}
			case response.StatusCode >= 200 && response.StatusCode < 300:
				return response, responseBody, nil
			default:
				lastErr = newAPIError(response.StatusCode, responseBody, response.Header, method+" "+endpoint)
			}
		}

		if !policy.shouldRetry(lastErr) || attempt >= policy.MaxRetries {
			return nil, nil, lastErr
		}
		delay := policy.delay(lastErr, attempt+1)
		if policy.TotalTimeout > 0 && time.Since(start)+delay > policy.TotalTimeout {
			return nil, nil, lastErr
		}
		c.logger.Info("typesafe retry", "attempt", attempt+1, "delay", delay, "error", lastErr)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
}

// transportError classifies an error returned by the HTTP client.
func transportError(err error, timeout time.Duration) error {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &TimeoutError{Timeout: timeout, Err: err}
	}
	return &ConnectionError{Err: err}
}

// runtimeInfo describes the Go runtime for the X-TypeSafe-Runtime header.
func runtimeInfo() string {
	return fmt.Sprintf("go/%s (%s; %s)", strings.TrimPrefix(runtime.Version(), "go"), runtime.GOOS, runtime.GOARCH)
}
