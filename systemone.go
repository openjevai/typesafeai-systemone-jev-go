package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SystemOneRequest is a state plus the questions to evaluate against it.
// Every question sees the same state and is answered independently.
type SystemOneRequest struct {
	// State is the content all questions refer to: a string, or structured
	// data as a map[string]any, []any, or a struct that marshals to an
	// object. Name fields in question instructions with backticked paths
	// such as `ticket.messages[0].text` to point at the relevant part.
	State any
	// Model optionally overrides the client's default model for this
	// request. Use ModelJevLatest, ModelJevPreview, or a pinned ID.
	Model string
	// Questions maps question IDs to questions. At least one is required.
	Questions Questions
	// Extra merges additional top-level request fields into the JSON body.
	// It is an escape hatch for API features newer than this SDK; its keys
	// override State, Model, and Questions.
	Extra map[string]any
}

// MarshalJSON writes the request body in the shape the System One API
// expects, merging Extra last.
func (r SystemOneRequest) MarshalJSON() ([]byte, error) {
	state, err := marshalState(r.State)
	if err != nil {
		return nil, err
	}
	body := make(map[string]any, 3+len(r.Extra))
	body["state"] = state
	if r.Model != "" {
		body["model"] = r.Model
	}
	body["questions"] = r.Questions
	for key, value := range r.Extra {
		body[key] = value
	}
	return json.Marshal(body)
}

// validate checks the request before anything reaches the network.
func (r SystemOneRequest) validate() error {
	if _, err := marshalState(r.State); err != nil {
		return err
	}
	if len(r.Questions) == 0 {
		return fmt.Errorf("%w: at least one question is required", ErrInvalidRequest)
	}
	for id, question := range r.Questions {
		if id == "" {
			return fmt.Errorf("%w: question IDs must not be empty", ErrInvalidRequest)
		}
		if err := validateQuestion(question); err != nil {
			return fmt.Errorf("%w: question %q: %v", ErrInvalidRequest, id, err)
		}
	}
	return nil
}

// marshalState encodes the state and enforces the API's accepted shapes: a
// string, an object, or an array.
func marshalState(state any) (json.RawMessage, error) {
	if state == nil {
		return nil, fmt.Errorf("%w: state is required", ErrInvalidRequest)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("%w: state is not JSON-encodable: %v", ErrInvalidRequest, err)
	}
	switch raw[0] {
	case '"', '{', '[':
		return raw, nil
	default:
		return nil, fmt.Errorf("%w: state must be a string, object, or array", ErrInvalidRequest)
	}
}

// SystemOne evaluates every question against the state in one request. The
// questions run in parallel inside TypeSafe and cannot see one another's
// answers; when a later judgment depends on an earlier answer, make a second
// call from code.
//
// The returned response is guaranteed to contain one answer per question
// unless the API returns an answer kind this SDK does not recognize, which is
// preserved as UnknownAnswer.
func (c *Client) SystemOne(ctx context.Context, req SystemOneRequest, opts ...CallOption) (*SystemOneResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	cc, err := resolveCallOptions(opts)
	if err != nil {
		return nil, err
	}
	if req.Model == "" {
		req.Model = c.model
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	response, responseBody, err := c.do(ctx, http.MethodPost, pathSystemOne, body, cc)
	if err != nil {
		return nil, err
	}
	result := &SystemOneResponse{RequestID: response.Header.Get(headerRequestID)}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return nil, &InvalidResponseError{StatusCode: response.StatusCode, Body: responseBody, Err: err}
	}
	return result, nil
}

// SystemOneRequest always marshals through its custom encoder.
var _ json.Marshaler = SystemOneRequest{}
