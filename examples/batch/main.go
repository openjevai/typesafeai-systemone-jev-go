// Command batch evaluates many documents against the same questions.
//
// It shows both Go wrappers: SystemOneAsync for a single call that runs while
// the program does other work, and StreamSystemOne for a bounded pool of
// calls whose results arrive as they finish.
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/batch
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	jev "github.com/mheers/typesafeai-systemone-jev-go"
)

var messages = []string{
	"I was charged twice for order A-104. Please refund the duplicate.",
	"The app crashes whenever I open the settings screen after the update.",
	"Can I switch to the annual plan and keep my current discount?",
	"The export button has been greyed out since yesterday. Is that intentional?",
	"Please cancel my subscription and delete my data.",
	"How do I invite a second admin to my workspace?",
}

func main() {
	client, err := jev.NewClient()
	if err != nil {
		if os.Getenv(jev.EnvAPIKey) == "" {
			log.Fatalf("set %s to run this example: %v", jev.EnvAPIKey, err)
		}
		log.Fatal(err)
	}
	ctx := context.Background()

	questions := func() jev.Questions {
		return jev.Questions{
			"department": jev.Choice{
				Instructions: "Which team should handle this?",
				Criteria: jev.Choices{
					"billing":   "Payment, invoice, or subscription issues",
					"technical": "Bugs, outages, or integration problems",
					"sales":     "Pricing, upgrades, or new accounts",
					"other":     "None of the above",
				},
			},
			"is_urgent": jev.Noul{
				Instructions: "Does this message convey urgency or time-sensitivity?",
			},
		}
	}

	// A single call in the background: do other work while it runs.
	future := client.SystemOneAsync(ctx, jev.SystemOneRequest{
		State:     messages[0],
		Questions: questions(),
	})
	time.Sleep(50 * time.Millisecond) // stand-in for useful work
	first, err := future.Result()
	if err != nil {
		log.Fatal(err)
	}
	if department, ok := first.Choice("department"); ok {
		fmt.Printf("#0 (async)       %-9s %s\n", department.Choice, truncate(messages[0]))
	}

	// Stream the rest with three calls in flight, printing as they finish.
	requests := make([]jev.SystemOneRequest, 0, len(messages)-1)
	for _, message := range messages[1:] {
		requests = append(requests, jev.SystemOneRequest{State: message, Questions: questions()})
	}
	results, err := client.StreamSystemOne(ctx, requests, jev.WithStreamConcurrency(3))
	if err != nil {
		log.Fatal(err)
	}
	for result := range results {
		if result.Err != nil {
			fmt.Printf("#%d (failed)      %v\n", result.Index+1, result.Err)
			continue
		}
		department, _ := result.Response.Choice("department")
		urgency, _ := result.Response.Noul("is_urgent")
		flag := "-"
		if urgency.Noul >= 0.9 {
			flag = "urgent"
		}
		fmt.Printf("#%d (streamed)    %-9s %-6s %s\n", result.Index+1, department.Choice, flag,
			truncate(requests[result.Index].State.(string)))
	}
}

// truncate keeps the sample output on one line.
func truncate(text string) string {
	if len(text) > 60 {
		return text[:57] + "..."
	}
	return text
}
