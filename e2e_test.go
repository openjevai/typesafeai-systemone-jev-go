package jev

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"
)

// End-to-end tests hit the live TypeSafe API and are skipped unless
// TYPESAFE_API_KEY is set. They are never run by CI.
func e2eClient(t *testing.T, opts ...Option) *Client {
	t.Helper()
	if os.Getenv(EnvAPIKey) == "" {
		t.Skipf("set %s to run end-to-end tests against the live TypeSafe API", EnvAPIKey)
	}
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode")
	}
	options := append([]Option{WithTimeout(30 * time.Second)}, opts...)
	client, err := NewClient(options...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

const e2eTicket = "Hi, I've been trying to connect my Stripe account for 3 days and it keeps failing. " +
	"I'm losing sales. Please help ASAP."

func TestE2E_ListModels(t *testing.T) {
	client := e2eClient(t)
	response, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(response.Models) == 0 {
		t.Fatal("no models returned")
	}
	found := false
	for _, model := range response.Models {
		if model.Name == ModelJevLatest {
			found = true
		}
		if model.Description == "" || model.ReleaseDate == "" {
			t.Errorf("model %+v is missing metadata", model)
		}
	}
	if !found {
		t.Errorf("models %+v should contain %q", response.Models, ModelJevLatest)
	}
	if response.RequestID == "" {
		t.Error("missing request id")
	}
}

func TestE2E_SystemOneTicketTriage(t *testing.T) {
	client := e2eClient(t)
	criteria := Choices{
		"billing":   "Payment, invoice, or subscription issues",
		"technical": "Bugs, outages, or integration problems",
		"sales":     "Pricing, upgrades, or new accounts",
		"other":     "None of the above",
	}
	levels := []any{"Calm, just stating facts", "Frustrated but civil", "Very angry, strong language"}

	response, err := client.SystemOne(context.Background(), SystemOneRequest{
		State: e2eTicket,
		Questions: Questions{
			"department":  Choice{Instructions: "Which team should handle this?", Criteria: criteria},
			"frustration": Score{Instructions: "How frustrated is the customer?", Criteria: levels},
			"is_urgent":   Noul{Instructions: "Does this message convey urgency or time-sensitivity?"},
			"is_billing":  Noul{Instructions: "Is this message about payment or subscription problems?"},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	if response.Model == "" {
		t.Error("response is missing the model name")
	}
	if response.RequestID == "" {
		t.Error("response is missing the request id")
	}
	if response.Usage.InputTokens <= 0 {
		t.Errorf("usage = %+v, want a positive input token count", response.Usage)
	}

	department, ok := response.Choice("department")
	if !ok {
		t.Fatalf("department answer missing: %+v", response.Answers)
	}
	if _, valid := criteria[department.Choice]; !valid {
		t.Errorf("choice %q is not one of the criteria", department.Choice)
	}
	if department.Confidence < 0 || department.Confidence > 1 {
		t.Errorf("confidence = %v", department.Confidence)
	}
	if sum := probabilitySum(department.Probabilities); math.Abs(sum-1) > 0.02 {
		t.Errorf("probabilities sum to %v: %v", sum, department.Probabilities)
	}

	frustration, ok := response.Score("frustration")
	if !ok {
		t.Fatalf("frustration answer missing: %+v", response.Answers)
	}
	if frustration.Score < 0 || frustration.Score > float64(len(levels)-1) {
		t.Errorf("score = %v, want within the levels", frustration.Score)
	}
	if len(frustration.Legend) != len(levels) {
		t.Errorf("legend = %v, want %d levels", frustration.Legend, len(levels))
	}

	urgency, ok := response.Noul("is_urgent")
	if !ok {
		t.Fatalf("is_urgent answer missing: %+v", response.Answers)
	}
	if urgency.Noul < 0 || urgency.Noul > 1 {
		t.Errorf("noul = %v, want within [0, 1]", urgency.Noul)
	}
	if urgency.Noul <= 0.5 {
		t.Errorf("urgency = %v, want an urgent ticket to score above 0.5", urgency.Noul)
	}

	billing, ok := response.Noul("is_billing")
	if !ok {
		t.Fatalf("is_billing answer missing: %+v", response.Answers)
	}
	if billing.Noul <= 0.5 {
		t.Errorf("is_billing = %v, want a Stripe message to score above 0.5", billing.Noul)
	}
}

func TestE2E_SystemOneStructuredState(t *testing.T) {
	client := e2eClient(t)
	state := map[string]any{
		"ticket_message": "My flight was cancelled. Can I get a refund?",
		"refund_policy":  "Cancelled flights are eligible for a full refund.",
	}

	response, err := client.SystemOne(context.Background(), SystemOneRequest{
		State: state,
		Questions: Questions{
			"refund_requested": Noul{
				Instructions: "Does `ticket_message` request a refund?",
			},
			"policy_supports_refund": Noul{
				Instructions: "Does `refund_policy` support the refund requested in `ticket_message`?",
			},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	requested, ok := response.Noul("refund_requested")
	if !ok || requested.Noul <= 0.5 {
		t.Errorf("refund_requested = %+v, %v; want a clear yes", requested, ok)
	}
	policy, ok := response.Noul("policy_supports_refund")
	if !ok || policy.Noul <= 0.5 {
		t.Errorf("policy_supports_refund = %+v, %v; want a clear yes", policy, ok)
	}
}

func TestE2E_SystemOneSpeculativeFanOut(t *testing.T) {
	client := e2eClient(t)
	review := "The battery lasts two days, which is great, but the charging port broke after a week. " +
		"Also the package arrived three days late. I want a replacement."

	response, err := client.SystemOne(context.Background(), SystemOneRequest{
		State: review,
		Questions: Questions{
			"sentiment": Choice{
				Instructions: "What is the overall sentiment?",
				Criteria:     Choices{"positive": nil, "negative": nil, "mixed": nil, "other": nil},
			},
			"rating": Score{
				Instructions: "How would a reviewer rate this product?",
				Criteria:     []any{"Terrible", "Poor", "Average", "Good", "Excellent"},
			},
			"mentions_battery":  Noul{Instructions: "Does the review mention the battery?"},
			"reports_defect":    Noul{Instructions: "Does the review report a defect or breakage?"},
			"shipping_problem":  Noul{Instructions: "Does the review complain about shipping or delivery?"},
			"wants_replacement": Noul{Instructions: "Does the review ask for a replacement?"},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	if len(response.Answers) != 6 {
		t.Fatalf("answers = %+v, want 6", response.Answers)
	}
	if len(response.Choices()) != 1 || len(response.Scores()) != 1 || len(response.Nouls()) != 4 {
		t.Errorf("answers by type: choices=%d scores=%d nouls=%d",
			len(response.Choices()), len(response.Scores()), len(response.Nouls()))
	}
	for name, answer := range response.Answers {
		if _, unknown := answer.(UnknownAnswer); unknown {
			t.Errorf("answer %q has an unrecognized type", name)
		}
	}
	if defect, ok := response.Noul("reports_defect"); !ok || defect.Noul <= 0.5 {
		t.Errorf("reports_defect = %+v, %v; want a clear yes", defect, ok)
	}
	if battery, ok := response.Noul("mentions_battery"); !ok || battery.Noul <= 0.5 {
		t.Errorf("mentions_battery = %+v, %v; want a clear yes", battery, ok)
	}
}

func TestE2E_AuthenticationError(t *testing.T) {
	if os.Getenv(EnvAPIKey) == "" {
		t.Skipf("set %s to run end-to-end tests against the live TypeSafe API", EnvAPIKey)
	}
	client, err := NewClient(
		WithAPIKey("e2e-invalid-key"),
		WithRetryPolicy(RetryPolicy{}),
		WithTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this a greeting?"}},
	})
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %v, want *AuthenticationError", err)
	}
}

func TestE2E_UnprocessableEntityError(t *testing.T) {
	client := e2eClient(t, WithRetryPolicy(RetryPolicy{}))
	// Extra overrides the SDK-built questions with an invalid choice
	// question, forcing the API to reject the request.
	_, err := client.SystemOne(context.Background(), SystemOneRequest{
		State:     "hello",
		Questions: Questions{"q": Noul{Instructions: "Is this a greeting?"}},
		Extra: map[string]any{
			"questions": map[string]any{"broken": map[string]any{"type": "choice"}},
		},
	})
	var unprocessable *UnprocessableEntityError
	if !errors.As(err, &unprocessable) {
		t.Fatalf("err = %v, want *UnprocessableEntityError", err)
	}
	if unprocessable.Message == "" {
		t.Error("validation error should carry the offending field")
	}
}

// probabilitySum adds up a probability distribution.
func probabilitySum(probabilities map[string]float64) float64 {
	sum := 0.0
	for _, probability := range probabilities {
		sum += probability
	}
	return sum
}
