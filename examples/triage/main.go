// Command triage shows composite scoring: several Score questions about one
// bug report, combined with weights in code, plus a Noul gate and
// confidence-based escalation.
//
// Run it with an API key in the environment:
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/triage
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

	report := `Our API integration started returning 500 errors on every request about 20 minutes ago, ` +
		`and we can't process any customer orders until this is fixed. ` +
		`Nothing changed on our side; we already checked our API key and our firewall rules.`

	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State: report,
		Questions: jev.Questions{
			"is_bug_report": jev.Noul{
				Instructions: "Does this report a bug or outage rather than ask a question or request a feature?",
			},
			"severity": jev.Score{
				Instructions: "How severe is the impact described?",
				Criteria: []any{
					"Cosmetic or minor inconvenience",
					"Some features degraded, workaround available",
					"Core workflow blocked for the customer",
					"Complete outage affecting all customers",
				},
			},
			"frustration": jev.Score{
				Instructions: "How frustrated does the reporter appear?",
				Criteria:     []any{"Calm", "Concerned", "Upset", "Angry"},
			},
			"reproducibility": jev.Score{
				Instructions: "How much does the report help an engineer reproduce and diagnose the problem?",
				Criteria:     []any{"No useful details", "Some context", "Clear steps or facts", "Detailed and precise"},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	isBug, _ := response.Noul("is_bug_report")
	severity, _ := response.Score("severity")
	frustration, _ := response.Score("frustration")
	reproducibility, _ := response.Score("reproducibility")

	// Weights are yours: change them without touching the questions.
	score := 0.5*severity.Score + 0.2*frustration.Score + 0.3*reproducibility.Score

	// Confidence guides whether to act automatically or fetch a human.
	confidence := severity.Confidence
	var route string
	switch {
	case isBug.Noul < 0.5:
		route = "route to general support"
	case confidence < 0.5 || severity.Score >= 2.5:
		route = "escalate to a human immediately"
	case score >= 2.0:
		route = "queue for the on-call engineer"
	default:
		route = "file as a normal ticket"
	}

	fmt.Printf("bug report:      %.2f\n", isBug.Noul)
	fmt.Printf("severity:        %.2f  (confidence %.2f)\n", severity.Score, severity.Confidence)
	fmt.Printf("frustration:     %.2f\n", frustration.Score)
	fmt.Printf("reproducibility: %.2f\n", reproducibility.Score)
	fmt.Printf("priority score:  %.2f\n", score)
	fmt.Printf("route:           %s\n", route)
}
