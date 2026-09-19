package jev

import (
	"context"
	"encoding/json"
	"net/http"
)

// ModelMetadata describes one model or alias accepted by the model field.
type ModelMetadata struct {
	// Name is the model ID or alias.
	Name string `json:"name"`
	// Description is a human-readable description of the model.
	Description string `json:"description"`
	// ReleaseDate is when the model or alias was released, as YYYY-MM-DD.
	ReleaseDate string `json:"release_date"`
}

// ModelsResponse lists the models available to the account.
type ModelsResponse struct {
	// Models holds one entry per model or alias.
	Models []ModelMetadata `json:"models"`
	// RequestID is the x-typesafe-request-id response header.
	RequestID string `json:"-"`
}

// ListModels returns the model names the account can send in the model field.
// The response currently lists the aliases; pinned IDs such as ModelJev1130
// are accepted whether or not they appear here.
func (c *Client) ListModels(ctx context.Context, opts ...CallOption) (*ModelsResponse, error) {
	cc, err := resolveCallOptions(opts)
	if err != nil {
		return nil, err
	}
	response, responseBody, err := c.do(ctx, http.MethodGet, pathModels, nil, cc)
	if err != nil {
		return nil, err
	}
	result := &ModelsResponse{RequestID: response.Header.Get(headerRequestID)}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return nil, &InvalidResponseError{StatusCode: response.StatusCode, Body: responseBody, Err: err}
	}
	return result, nil
}
