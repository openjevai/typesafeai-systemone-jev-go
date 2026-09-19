package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const minimalResponse = `{"model":"jev-latest","answers":{},"usage":{"input_tokens":10,"output_tokens":2}}`

func TestSystemOneSendsTypedRequest(t *testing.T) {
	var body map[string]any
	var header http.Header
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		body = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State: "Help! My payouts have been failing for 3 days.",
		Questions: Questions{
			"department": Choice{
				Instructions: "Which team should handle this?",
				Criteria: Choices{
					"billing":   "Payment or subscription issues",
					"technical": nil,
				},
			},
			"is_urgent": Noul{
				Instructions: "Does this convey urgency?",
				Criteria:     &NoulCriteria{True: "Explicitly time-sensitive", False: "No urgency"},
			},
			"frustration": Score{
				Instructions: "How frustrated is the customer?",
				Criteria:     []any{"Calm", "Frustrated", "Very angry"},
			},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	if body["state"] != "Help! My payouts have been failing for 3 days." {
		t.Errorf("state = %v", body["state"])
	}
	if body["model"] != DefaultModel {
		t.Errorf("model = %v, want %q", body["model"], DefaultModel)
	}
	questions := mustMap(t, body["questions"])
	if len(questions) != 3 {
		t.Fatalf("questions = %v, want 3 entries", questions)
	}
	department := mustMap(t, questions["department"])
	if department["type"] != TypeChoice {
		t.Errorf("department.type = %v", department["type"])
	}
	if department["instructions"] != "Which team should handle this?" {
		t.Errorf("department.instructions = %v", department["instructions"])
	}
	criteria := mustMap(t, department["criteria"])
	if criteria["billing"] != "Payment or subscription issues" || criteria["technical"] != nil {
		t.Errorf("department.criteria = %v", criteria)
	}
	urgency := mustMap(t, questions["is_urgent"])
	if urgency["type"] != TypeNoul {
		t.Errorf("is_urgent.type = %v", urgency["type"])
	}
	urgencyCriteria := mustMap(t, urgency["criteria"])
	if urgencyCriteria["true"] != "Explicitly time-sensitive" {
		t.Errorf("is_urgent.criteria = %v", urgencyCriteria)
	}
	frustration := mustMap(t, questions["frustration"])
	levels := mustSlice(t, frustration["criteria"])
	if len(levels) != 3 || levels[2] != "Very angry" {
		t.Errorf("frustration.criteria = %v", levels)
	}

	if got := header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	if got := header.Get("User-Agent"); !strings.HasPrefix(got, sdkName+"/") {
		t.Errorf("User-Agent = %q", got)
	}
	if got := header.Get("X-TypeSafe-SDK"); got != sdkName+"/"+Version {
		t.Errorf("X-TypeSafe-SDK = %q", got)
	}
	if got := header.Get("X-TypeSafe-Runtime"); !strings.HasPrefix(got, "go/") {
		t.Errorf("X-TypeSafe-Runtime = %q", got)
	}
	if got := header.Get("X-TypeSafe-Retry-Count"); got != "" {
		t.Errorf("X-TypeSafe-Retry-Count = %q, want absent on the first attempt", got)
	}
}

func TestSystemOneModelOverride(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Model:     ModelJev1130,
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if body["model"] != ModelJev1130 {
		t.Errorf("model = %v, want %q", body["model"], ModelJev1130)
	}
}

func TestSystemOneExtraMergesLast(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
		Extra: map[string]any{
			"beam_width": 4,
			"model":      "jev-future",
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if body["beam_width"] != float64(4) {
		t.Errorf("beam_width = %v, want 4", body["beam_width"])
	}
	if body["model"] != "jev-future" {
		t.Errorf("model = %v, want Extra to override", body["model"])
	}
}

func TestSystemOneStructuredState(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	state := map[string]any{
		"ticket": map[string]any{
			"subject":  "Duplicate charge",
			"messages": []any{map[string]any{"from": "customer", "text": "Please refund the duplicate."}},
		},
	}
	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     state,
		Questions: Questions{"refund": Noul{Instructions: "Does `ticket.messages[0].text` request a refund?"}},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	ticket := mustMap(t, body["state"])["ticket"]
	messages := mustSlice(t, mustMap(t, ticket)["messages"])
	if messages[0].(map[string]any)["text"] != "Please refund the duplicate." {
		t.Errorf("state did not round-trip: %v", body["state"])
	}
}

func TestSystemOneParsesEveryAnswerType(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(headerRequestID, "req_123")
		_, _ = w.Write([]byte(`{
			"model": "jev-1.13.0",
			"answers": {
				"is_urgent": {"type": "noul", "noul": 0.92},
				"department": {
					"type": "choice",
					"choice": "technical",
					"probabilities": {"billing": 0.08, "technical": 0.85, "sales": 0.07},
					"confidence": 0.82
				},
				"frustration": {
					"type": "score",
					"score": 1.6,
					"legend": {"0": "Calm", "1": "Frustrated", "2": "Very angry"},
					"probabilities": {"0": 0.05, "1": 0.3, "2": 0.65},
					"confidence": 0.78
				},
				"future": {"type": "multinomial", "labels": {"a": 0.6, "b": 0.4}}
			},
			"usage": {"input_tokens": 312, "output_tokens": 48}
		}`))
	})

	response, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"is_urgent": Noul{Instructions: "?"}},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if response.Model != "jev-1.13.0" {
		t.Errorf("Model = %q", response.Model)
	}
	if response.RequestID != "req_123" {
		t.Errorf("RequestID = %q", response.RequestID)
	}
	if response.Usage.InputTokens != 312 || response.Usage.OutputTokens != 48 {
		t.Errorf("Usage = %+v", response.Usage)
	}

	urgency, ok := response.Noul("is_urgent")
	if !ok || urgency.Noul != 0.92 {
		t.Errorf("Noul(is_urgent) = %+v, %v", urgency, ok)
	}
	department, ok := response.Choice("department")
	if !ok || department.Choice != "technical" || department.Confidence != 0.82 {
		t.Errorf("Choice(department) = %+v, %v", department, ok)
	}
	if department.Probabilities["billing"] != 0.08 {
		t.Errorf("department probabilities = %v", department.Probabilities)
	}
	frustration, ok := response.Score("frustration")
	if !ok || frustration.Score != 1.6 || frustration.Confidence != 0.78 {
		t.Errorf("Score(frustration) = %+v, %v", frustration, ok)
	}
	if frustration.Legend[2] != "Very angry" {
		t.Errorf("legend = %v", frustration.Legend)
	}
	if frustration.Probabilities[2] != 0.65 {
		t.Errorf("score probabilities = %v", frustration.Probabilities)
	}
	if len(response.Nouls()) != 1 || len(response.Choices()) != 1 || len(response.Scores()) != 1 {
		t.Errorf("typed maps: nouls=%d choices=%d scores=%d",
			len(response.Nouls()), len(response.Choices()), len(response.Scores()))
	}

	unknown, ok := response.Answers["future"].(UnknownAnswer)
	if !ok {
		t.Fatalf("future answer = %T, want UnknownAnswer", response.Answers["future"])
	}
	if unknown.Type != "multinomial" {
		t.Errorf("unknown type = %q", unknown.Type)
	}
	if !strings.Contains(string(unknown.Raw), `"labels"`) {
		t.Errorf("unknown raw = %s", unknown.Raw)
	}
	if _, ok := response.Noul("is_urgent"); !ok {
		t.Error("accessor failed for known answer")
	}
	if _, ok := response.Choice("is_urgent"); ok {
		t.Error("Choice accessor should not match a noul answer")
	}
	if _, ok := response.Answer("missing"); ok {
		t.Error("Answer accessor should miss unknown IDs")
	}
}

func TestSystemOneValidationErrorsNeverTouchTheNetwork(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	valid := Noul{Instructions: "Is this fine?"}
	tests := map[string]SystemOneRequest{
		"nil state":       {Questions: Questions{"q": valid}},
		"scalar state":    {State: 42, Questions: Questions{"q": valid}},
		"empty questions": {State: "hello"},
		"empty question ID": {
			State:     "hello",
			Questions: Questions{"": valid},
		},
		"nil question": {
			State:     "hello",
			Questions: Questions{"q": nil},
		},
		"choice without criteria": {
			State:     "hello",
			Questions: Questions{"q": Choice{Instructions: "Which?"}},
		},
		"score without criteria": {
			State:     "hello",
			Questions: Questions{"q": Score{Instructions: "How much?"}},
		},
		"raw question without type": {
			State:     "hello",
			Questions: Questions{"q": RawQuestion{"instructions": "Which?"}},
		},
		"raw choice without criteria": {
			State:     "hello",
			Questions: Questions{"q": RawQuestion{"type": TypeChoice}},
		},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := client.SystemOne(context.Background(), request)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Errorf("server saw %d calls, want 0", calls.Load())
	}
}

func TestSystemOneMapsAPIErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantAs  func(error) bool
		message string
	}{
		{
			name:   "unauthorized",
			status: http.StatusUnauthorized,
			body:   `{"detail":"Invalid API key"}`,
			wantAs: func(err error) bool {
				var target *AuthenticationError
				return errors.As(err, &target)
			},
		},
		{
			name:   "unprocessable entity",
			status: http.StatusUnprocessableEntity,
			body:   `{"detail":[{"loc":["body","questions","urgency","criteria"],"msg":"Field required","type":"missing"}]}`,
			wantAs: func(err error) bool {
				var target *UnprocessableEntityError
				return errors.As(err, &target)
			},
		},
		{
			name:   "internal server error",
			status: http.StatusInternalServerError,
			body:   `{"message":"something broke"}`,
			wantAs: func(err error) bool {
				var target *InternalServerError
				return errors.As(err, &target)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set(headerRequestID, "req_err")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			_, err := client.SystemOne(context.Background(), SystemOneRequest{
				State:     "hello",
				Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v (%T), want *APIError", err, err)
			}
			if apiErr.StatusCode != test.status {
				t.Errorf("status = %d, want %d", apiErr.StatusCode, test.status)
			}
			if apiErr.RequestID != "req_err" {
				t.Errorf("RequestID = %q", apiErr.RequestID)
			}
			if !test.wantAs(err) {
				t.Errorf("errors.As failed for %v", err)
			}
			if !strings.Contains(err.Error(), "req_err") {
				t.Errorf("error %q should mention the request id", err)
			}
			if !strings.Contains(err.Error(), "POST") || !strings.Contains(err.Error(), pathSystemOne) {
				t.Errorf("error %q should name the endpoint", err)
			}
		})
	}
}

func TestSystemOneErrorMessages(t *testing.T) {
	tests := map[string]string{
		"detail list":   "questions.urgency.criteria: Field required",
		"error string":  "quota exceeded",
		"error object":  "nested message",
		"plain text":    "gateway timeout",
		"message field": "top level message",
		"empty body":    "",
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			status := http.StatusUnprocessableEntity
			body := map[string]string{
				"detail list":   `{"detail":[{"loc":["body","questions","urgency","criteria"],"msg":"Field required"}]}`,
				"error string":  `{"error":"quota exceeded"}`,
				"error object":  `{"error":{"message":"nested message"}}`,
				"plain text":    `gateway timeout`,
				"message field": `{"message":"top level message"}`,
				"empty body":    ``,
			}[name]
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
			})
			_, err := client.SystemOne(context.Background(), SystemOneRequest{
				State:     "hello",
				Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
			})
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Message != want {
				t.Errorf("message = %q, want %q", apiErr.Message, want)
			}
		})
	}
}

func TestSystemOneInvalidResponseBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":`))
	})
	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	var invalid *InvalidResponseError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidResponseError", err)
	}
	if invalid.StatusCode != http.StatusOK {
		t.Errorf("status = %d", invalid.StatusCode)
	}
}

func TestSystemOneMalformedKnownAnswer(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul"}},"usage":{}}`))
	})
	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	var invalid *InvalidResponseError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidResponseError", err)
	}
	if !strings.Contains(err.Error(), "noul") {
		t.Errorf("error %q should identify the offending answer", err)
	}
}

func TestSystemOneCallOptions(t *testing.T) {
	var attempts atomic.Int32
	var retryCount, callHeader string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"transient"}`))
			return
		}
		retryCount = r.Header.Get("X-TypeSafe-Retry-Count")
		callHeader = r.Header.Get("X-Call")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	})

	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	},
		WithCallRetryPolicy(RetryPolicy{
			MaxRetries:             1,
			BackoffInitial:         time.Millisecond,
			BackoffMax:             time.Millisecond,
			RetryOnConnectionError: true,
			HTTPStatuses:           defaultRetryStatuses(),
		}),
		WithCallHeader("X-Call", "yes"),
	)
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
	if retryCount != "1" {
		t.Errorf("retry count header = %q, want %q", retryCount, "1")
	}
	if callHeader != "yes" {
		t.Errorf("call header = %q", callHeader)
	}
}

func TestSystemOneContextCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithTimeout(0),
		WithRetryPolicy(RetryPolicy{MaxRetries: 5, TotalTimeout: time.Minute}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err = client.SystemOne(ctx, SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestListModels(t *testing.T) {
	var method, path string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(headerRequestID, "req_models")
		_, _ = w.Write([]byte(`{"models":[
			{"name":"jev-latest","description":"General-purpose system one model.","release_date":"2026-09-15"},
			{"name":"jev-1.13.0","description":"Pinned release.","release_date":"2026-09-15"}
		]}`))
	})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if method != http.MethodGet || path != pathModels {
		t.Errorf("request = %s %s, want GET %s", method, path, pathModels)
	}
	if models.RequestID != "req_models" {
		t.Errorf("RequestID = %q", models.RequestID)
	}
	if len(models.Models) != 2 || models.Models[0].Name != "jev-latest" || models.Models[0].ReleaseDate != "2026-09-15" {
		t.Errorf("models = %+v", models.Models)
	}
}

func TestListModelsInvalidResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":"nope"}`))
	})
	_, err := client.ListModels(context.Background())
	var invalid *InvalidResponseError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidResponseError", err)
	}
}

func TestRequestMarshalOrderIsStable(t *testing.T) {
	request := SystemOneRequest{
		State:     "hello",
		Model:     "jev-latest",
		Questions: Questions{"q": Noul{Instructions: "Is this hello?"}},
		Extra:     map[string]any{"alpha": 1, "beta": 2},
	}
	first, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for index := 0; index < 10; index++ {
		next, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if string(first) != string(next) {
			t.Fatalf("marshal output changed between runs:\n%s\n%s", first, next)
		}
	}
}
