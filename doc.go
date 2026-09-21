// Package jev is a Go client for the TypeSafe System One API, home of the
// Jev family of models.
//
// System One models turn natural language and application state into typed
// judgments that ordinary code can consume. Code owns the workflow; the model
// supplies programmable common sense where semantic understanding is needed.
// There are three question primitives, and you can mix them freely in a
// single request:
//
//   - Choice selects one option from a set you define.
//   - Score rates the state along ordered, described levels.
//   - Noul returns the probability that a yes/no statement is true.
//
// Every question is evaluated independently against the same state, so a
// request with many questions costs little more than a request with one and
// returns in about the same time. Ask every question the code might need,
// including speculative ones, and use the answers you care about.
//
// Create a client with NewClient, which reads the TYPESAFE_API_KEY
// environment variable, then ask any number of questions about one state:
//
//	client, err := jev.NewClient()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	response, err := client.SystemOne(ctx, jev.SystemOneRequest{
//		State: "Hi, I've been trying to connect my Stripe account for 3 days and it keeps failing.",
//		Questions: jev.Questions{
//			"department": jev.Choice{
//				Instructions: "Which team should handle this?",
//				Criteria: jev.Choices{
//					"billing":   "Payment or subscription issues",
//					"technical": "Bugs or integration problems",
//				},
//			},
//			"is_urgent": jev.Noul{
//				Instructions: "Does this message convey urgency?",
//			},
//		},
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	if department, ok := response.Choice("department"); ok {
//		fmt.Println(department.Choice) // "billing"
//	}
//	if urgency, ok := response.Noul("is_urgent"); ok {
//		fmt.Println(urgency.Noul) // e.g. 0.98
//	}
//
// Answers are typed: a Choice answer always names one of your options and
// carries the full probability distribution, a Score answer always lands on
// your rubric, and a Noul answer is a number between 0 and 1. Probabilities
// and confidence guide thresholds and escalation, which your code owns.
//
// The response also keeps the exact body the API returned: Raw is that body
// and RawAnswer returns one answer object verbatim, for audit, calibration and
// raw-answer storage.
//
// The client performs retries with exponential backoff and honors Retry-After
// on rate limits and overloads. Errors are typed and inspectable with
// errors.As; see APIError, RateLimitError, TimeoutError, and ConnectionError.
//
// For many independent calls, SystemOneAsync runs one in the background and
// StreamSystemOne runs a bounded pool whose results arrive as they finish:
//
//	results, err := client.StreamSystemOne(ctx, requests, jev.WithStreamConcurrency(8))
//	if err != nil {
//		log.Fatal(err)
//	}
//	for result := range results {
//		if result.Err != nil {
//			log.Printf("request %d failed: %v", result.Index, result.Err)
//			continue
//		}
//		fmt.Println(result.Index, result.Response.Model)
//	}
//
// The cmd/jev command uses the same client from the shell.
//
// This package has no third-party dependencies.
package jev
