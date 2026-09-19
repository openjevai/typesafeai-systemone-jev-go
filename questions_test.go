package jev

import (
	"encoding/json"
	"reflect"
	"testing"
)

func marshalToAny(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", raw, err)
	}
	return decoded
}

func TestNoulMarshal(t *testing.T) {
	question := marshalToAny(t, Noul{Instructions: "Does this convey urgency?"})
	want := map[string]any{
		"type":         "noul",
		"instructions": "Does this convey urgency?",
	}
	if !reflect.DeepEqual(question, want) {
		t.Errorf("noul = %v, want %v", question, want)
	}

	withCriteria := marshalToAny(t, Noul{
		Instructions: "Is this spam?",
		Criteria:     &NoulCriteria{True: "Unsolicited advertising", False: nil},
	})
	wantCriteria := map[string]any{
		"type":         "noul",
		"instructions": "Is this spam?",
		"criteria":     map[string]any{"true": "Unsolicited advertising"},
	}
	if !reflect.DeepEqual(withCriteria, wantCriteria) {
		t.Errorf("noul with criteria = %v, want %v", withCriteria, wantCriteria)
	}
}

func TestChoiceMarshal(t *testing.T) {
	question := marshalToAny(t, Choice{
		Instructions: "Which team should handle this?",
		Criteria: Choices{
			"billing":   "Payment or subscription issues",
			"technical": nil,
		},
	})
	want := map[string]any{
		"type":         "choice",
		"instructions": "Which team should handle this?",
		"criteria": map[string]any{
			"billing":   "Payment or subscription issues",
			"technical": nil,
		},
	}
	if !reflect.DeepEqual(question, want) {
		t.Errorf("choice = %v, want %v", question, want)
	}
}

func TestScoreMarshal(t *testing.T) {
	question := marshalToAny(t, Score{
		Instructions: "How frustrated is the customer?",
		Criteria:     []any{"Calm", "Frustrated", "Very angry"},
	})
	want := map[string]any{
		"type":         "score",
		"instructions": "How frustrated is the customer?",
		"criteria":     []any{"Calm", "Frustrated", "Very angry"},
	}
	if !reflect.DeepEqual(question, want) {
		t.Errorf("score = %v, want %v", question, want)
	}
}

func TestStructuredInstructionsMarshal(t *testing.T) {
	question := marshalToAny(t, Noul{
		Instructions: map[string]any{"task": "Identify unsolicited advertising.", "language": "en"},
	})
	instructions := mustMap(t, question["instructions"])
	if instructions["task"] != "Identify unsolicited advertising." {
		t.Errorf("instructions = %v", instructions)
	}

	arrayQuestion := marshalToAny(t, Score{
		Instructions: []any{"Rate the message.", "Use the full rubric."},
		Criteria:     []any{"a", "b"},
	})
	if len(mustSlice(t, arrayQuestion["instructions"])) != 2 {
		t.Errorf("array instructions = %v", arrayQuestion["instructions"])
	}
}

func TestRawQuestionMarshal(t *testing.T) {
	question := marshalToAny(t, RawQuestion{
		"type":         "choice",
		"instructions": "Which language is this?",
		"criteria":     map[string]any{"go": nil, "rust": nil},
		"weight":       2,
	})
	if question["type"] != "choice" || question["weight"] != float64(2) {
		t.Errorf("raw question = %v", question)
	}
}

func TestQuestionType(t *testing.T) {
	tests := map[string]Question{
		TypeNoul:   Noul{},
		TypeChoice: Choice{Criteria: Choices{"a": nil}},
		TypeScore:  Score{Criteria: []any{"a"}},
	}
	for want, question := range tests {
		if got := question.questionType(); got != want {
			t.Errorf("questionType() = %q, want %q", got, want)
		}
	}
	raw := RawQuestion{"type": "future"}
	if got := raw.questionType(); got != "future" {
		t.Errorf("raw questionType() = %q", got)
	}
}

func TestValidateQuestion(t *testing.T) {
	tests := map[string]struct {
		question Question
		wantErr  bool
	}{
		"noul":                        {Noul{Instructions: "?"}, false},
		"choice":                      {Choice{Criteria: Choices{"a": nil}}, false},
		"score":                       {Score{Criteria: []any{"a"}}, false},
		"choice without criteria":     {Choice{}, true},
		"score without criteria":      {Score{}, true},
		"nil question":                {nil, true},
		"raw question":                {RawQuestion{"type": "noul"}, false},
		"raw without type":            {RawQuestion{}, true},
		"raw choice without criteria": {RawQuestion{"type": "choice"}, true},
		"raw score empty criteria":    {RawQuestion{"type": "score", "criteria": []any{}}, true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateQuestion(test.question)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateQuestion() = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestNilPointerQuestion(t *testing.T) {
	var question *Noul
	if err := validateQuestion(question); err == nil {
		t.Fatal("expected an error for a typed nil question")
	}
}
