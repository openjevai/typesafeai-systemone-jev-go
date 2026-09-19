// Command rerank scores a shortlist of passages against a query in one
// request: one Score question per candidate, then the ranking happens in
// code. It also asks whether the shortlist contains an answer at all.
//
// This is the rerank_typesafe pattern: retrieval produces candidates, one
// TypeSafe call grades them, code selects what the answering model sees.
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/rerank
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

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

	query := "How do I rotate an API key without downtime?"

	// A stand-in for the shortlist a retrieval step would produce.
	passages := []string{
		"API keys can be created and revoked from the dashboard under Settings > Keys.",
		"To rotate a key without downtime: create a second key, deploy it to your services, " +
			"watch for traffic on the old key to reach zero, then revoke the old key.",
		"The free plan includes two API keys; the team plan includes twenty.",
		"Revoked keys stop working immediately, so deploy the replacement before revoking the old key.",
		"Our status page reports API availability and incident history.",
	}

	state := map[string]any{"query": query, "passages": passages}
	questions := jev.Questions{
		"contains_answer": jev.Noul{
			Instructions: "Do the passages in `passages` contain an answer to the question in `query`?",
		},
	}
	levels := []any{
		"Irrelevant to the query",
		"Related, but does not help answer it",
		"Useful context for the answer",
		"Directly answers the query",
	}
	for index := range passages {
		questions[fmt.Sprintf("p%d", index)] = jev.Score{
			Instructions: fmt.Sprintf("How well does `passages[%d]` answer the question in `query`?", index),
			Criteria:     levels,
		}
	}

	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State:     state,
		Questions: questions,
	})
	if err != nil {
		log.Fatal(err)
	}

	if contains, ok := response.Noul("contains_answer"); ok {
		fmt.Printf("shortlist contains an answer: %.2f\n\n", contains.Noul)
	}

	type ranked struct {
		index int
		score jev.ScoreAnswer
	}
	ranking := make([]ranked, 0, len(passages))
	for index := range passages {
		if score, ok := response.Score(fmt.Sprintf("p%d", index)); ok {
			ranking = append(ranking, ranked{index: index, score: score})
		}
	}
	sort.SliceStable(ranking, func(i, j int) bool {
		return ranking[i].score.Score > ranking[j].score.Score
	})

	fmt.Println("ranking (highest score first):")
	for position, item := range ranking {
		text := passages[item.index]
		if len(text) > 76 {
			text = text[:73] + "..."
		}
		fmt.Printf("  %d. p%d  score %.2f  confidence %.2f  %s\n",
			position+1, item.index, item.score.Score, item.score.Confidence, strings.TrimSpace(text))
	}
}
