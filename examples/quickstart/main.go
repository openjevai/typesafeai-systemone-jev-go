// Command quickstart asks three typed questions about a support ticket and
// prints the answers, showing the shape of a System One request.
//
// Run it with an API key in the environment:
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/quickstart
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	jev "github.com/mheers/typesafeai-systemone-jev-go"
)

func main() {
	client, err := jev.NewClient()
	if err != nil {
		if os.Getenv(jev.EnvAPIKey) == "" {
			log.Fatalf("set %s to run this example: %v", jev.EnvAPIKey, err)
		}
		log.Fatal(err)
	}

	ticket := "Hi, I've been trying to connect my Stripe account for 3 days and it keeps failing. " +
		"I'm losing sales. Please help ASAP."

	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State: ticket,
		Questions: jev.Questions{
			"department": jev.Choice{
				Instructions: "Which team should handle this?",
				Criteria: jev.Choices{
					"billing":   "Payment or subscription issues",
					"technical": "Bugs or integration problems",
					"sales":     "Pricing or account questions",
				},
			},
			"frustration": jev.Score{
				Instructions: "How frustrated is the customer?",
				Criteria:     []any{"Calm, just stating facts", "Frustrated but civil", "Very angry, strong language"},
			},
			"is_urgent": jev.Noul{
				Instructions: "Does this message convey urgency or time-sensitivity?",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	department, _ := response.Choice("department")
	frustration, _ := response.Score("frustration")
	urgency, _ := response.Noul("is_urgent")

	fmt.Printf("department:  %-10s (confidence %.2f)\n", department.Choice, department.Confidence)
	fmt.Printf("frustration: %.2f          (confidence %.2f, legend %v)\n", frustration.Score, frustration.Confidence, frustration.Legend)
	fmt.Printf("is urgent:   %.2f\n", urgency.Noul)
	fmt.Printf("model:       %s\n", response.Model)
	fmt.Printf("usage:       %d input tokens, %d output tokens\n", response.Usage.InputTokens, response.Usage.OutputTokens)
}
