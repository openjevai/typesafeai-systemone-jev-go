// Command extraction pulls one value out of a document without trusting the
// model to produce it: code finds candidate spans with a regex, one Choice
// question selects the candidate that matches the request, and code copies
// the verbatim span and normalizes it.
//
// This is the pre_parsed_value_extraction pattern: retrieval produces
// candidates, TypeSafe selects among them, and the value never round-trips
// through generated text.
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/extraction
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	jev "github.com/mheers/typesafeai-systemone-jev-go"
)

// amountPattern finds currency amounts such as $1,240.00 or $89.
var amountPattern = regexp.MustCompile(`\$[0-9][0-9,]*(?:\.[0-9]{2})?`)

func main() {
	client, err := jev.NewClient()
	if err != nil {
		if os.Getenv(jev.EnvAPIKey) == "" {
			log.Fatalf("set %s to run this example: %v", jev.EnvAPIKey, err)
		}
		log.Fatal(err)
	}

	document := `Invoice 2026-0917
Two standing desks at $640.00 each: $1,280.00
Shipping: $89.00
Discount: -$120.00
Total amount due: $1,249.00
Payment is due within 30 days.`

	// Code finds every candidate span: the model can only choose one of
	// these, it cannot invent an amount.
	candidates := amountPattern.FindAllString(document, -1)
	if len(candidates) == 0 {
		fmt.Println("no amounts found in the document")
		return
	}
	criteria := make(jev.Choices, len(candidates))
	options := make([]map[string]string, len(candidates))
	for index, candidate := range candidates {
		id := fmt.Sprintf("c%d", index)
		criteria[id] = candidate
		options[index] = map[string]string{"id": id, "value": candidate}
	}

	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State: map[string]any{"document": document, "amount_candidates": options},
		Questions: jev.Questions{
			"has_total": jev.Noul{
				Instructions: "Does `document` state the total amount due?",
			},
			"total_candidate": jev.Choice{
				Instructions: "Which candidate in `amount_candidates` is the total amount due in `document`?",
				Criteria:     criteria,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	hasTotal, _ := response.Noul("has_total")
	if hasTotal.Noul < 0.5 {
		fmt.Printf("the document does not state a total amount due (%.2f)\n", hasTotal.Noul)
		return
	}
	selected, ok := response.Choice("total_candidate")
	if !ok {
		log.Fatal("no choice answer returned")
	}

	// Copy the verbatim span the model selected and normalize it in code.
	verbatim := criteria[selected.Choice].(string)
	amount, err := parseAmount(verbatim)
	if err != nil {
		log.Fatalf("selected span %q: %v", verbatim, err)
	}

	fmt.Printf("candidates: %s\n", strings.Join(candidates, "  "))
	fmt.Printf("selected:   %s (confidence %.2f)\n", verbatim, selected.Confidence)
	fmt.Printf("normalized: %.2f\n", amount)
}

// parseAmount converts a currency span into a number.
func parseAmount(span string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(strings.TrimPrefix(span, "$"), ",", ""), 64)
}
