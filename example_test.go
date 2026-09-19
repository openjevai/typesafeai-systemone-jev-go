package jev_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	jev "github.com/mheers/typesafeai-systemone-jev-go"
)

// stubTransport replays a canned response so the examples run without a key.
type stubTransport struct {
	status int
	body   string
}

func (s stubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    request,
	}, nil
}

func ExampleClient_SystemOne() {
	client, err := jev.NewClient(
		jev.WithAPIKey("example-key"),
		jev.WithHTTPClient(&http.Client{Transport: stubTransport{body: `{
			"model": "jev-latest",
			"answers": {
				"department": {
					"type": "choice",
					"choice": "billing",
					"probabilities": {"billing": 0.84, "technical": 0.16},
					"confidence": 0.68
				},
				"is_urgent": {"type": "noul", "noul": 0.99}
			},
			"usage": {"input_tokens": 312, "output_tokens": 12}
		}`}}),
	)
	if err != nil {
		panic(err)
	}

	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State: "Hi, I've been trying to connect my Stripe account for 3 days and it keeps failing. Please help ASAP.",
		Questions: jev.Questions{
			"department": jev.Choice{
				Instructions: "Which team should handle this?",
				Criteria: jev.Choices{
					"billing":   "Payment or subscription issues",
					"technical": "Bugs or integration problems",
				},
			},
			"is_urgent": jev.Noul{
				Instructions: "Does this message convey urgency?",
			},
		},
	})
	if err != nil {
		panic(err)
	}

	department, _ := response.Choice("department")
	fmt.Printf("department: %s (confidence %.2f)\n", department.Choice, department.Confidence)

	urgency, _ := response.Noul("is_urgent")
	if urgency.Noul >= 0.9 {
		fmt.Println("route: escalate")
	}
	fmt.Printf("input tokens: %d\n", response.Usage.InputTokens)

	// Output:
	// department: billing (confidence 0.68)
	// route: escalate
	// input tokens: 312
}

func ExampleClient_ListModels() {
	client, err := jev.NewClient(
		jev.WithAPIKey("example-key"),
		jev.WithHTTPClient(&http.Client{Transport: stubTransport{body: `{"models":[
			{"name":"jev-latest","description":"General-purpose system one model.","release_date":"2026-09-15"}
		]}`}}),
	)
	if err != nil {
		panic(err)
	}

	models, err := client.ListModels(context.Background())
	if err != nil {
		panic(err)
	}
	for _, model := range models.Models {
		fmt.Printf("%s (%s): %s\n", model.Name, model.ReleaseDate, model.Description)
	}

	// Output:
	// jev-latest (2026-09-15): General-purpose system one model.
}

func ExampleRateLimitError() {
	client, err := jev.NewClient(
		jev.WithAPIKey("example-key"),
		jev.WithRetryPolicy(jev.RetryPolicy{}), // return the first failure instead of retrying
		jev.WithHTTPClient(&http.Client{Transport: stubTransport{
			status: http.StatusTooManyRequests,
			body:   `{"message":"rate limit exceeded"}`,
		}}),
	)
	if err != nil {
		panic(err)
	}

	_, err = client.SystemOne(context.Background(), jev.SystemOneRequest{
		State:     "hello",
		Questions: jev.Questions{"greeting": jev.Noul{Instructions: "Is this a greeting?"}},
	})

	var rateLimit *jev.RateLimitError
	if errors.As(err, &rateLimit) {
		fmt.Printf("rate limited: %d %s\n", rateLimit.StatusCode, rateLimit.Message)
	}

	// Output:
	// rate limited: 429 rate limit exceeded
}
