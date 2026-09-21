package jev

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAnswersUnmarshal(t *testing.T) {
	raw := `{
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
		"future": {"type": "ranking", "ranking": ["a", "b"]}
	}`
	var answers Answers
	if err := json.Unmarshal([]byte(raw), &answers); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(answers) != 4 {
		t.Fatalf("answers = %v, want 4 entries", answers)
	}
	if answer := answers["is_urgent"].(NoulAnswer); answer.Noul != 0.92 {
		t.Errorf("noul = %+v", answer)
	}
	if answer := answers["department"].(ChoiceAnswer); answer.Choice != "technical" || answer.Confidence != 0.82 {
		t.Errorf("choice = %+v", answer)
	}
	if answer := answers["frustration"].(ScoreAnswer); answer.Score != 1.6 || answer.Legend[2] != "Very angry" {
		t.Errorf("score = %+v", answer)
	}
	unknown, ok := answers["future"].(UnknownAnswer)
	if !ok || unknown.Type != "ranking" {
		t.Fatalf("future = %#v, want UnknownAnswer", answers["future"])
	}
}

func TestAnswersMarshalRoundTrip(t *testing.T) {
	answers := Answers{
		"is_urgent": NoulAnswer{Noul: 0.92},
		"department": ChoiceAnswer{
			Choice:        "technical",
			Probabilities: map[string]float64{"billing": 0.08, "technical": 0.92},
			Confidence:    0.8,
		},
		"frustration": ScoreAnswer{
			Score:         1.6,
			Legend:        map[int]any{0: "Calm", 1: "Frustrated"},
			Probabilities: map[int]float64{0: 0.4, 1: 0.6},
			Confidence:    0.7,
		},
		"future": UnknownAnswer{Type: "ranking", Raw: json.RawMessage(`{"type":"ranking","ranking":["a"]}`)},
	}
	raw, err := json.Marshal(answers)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", raw, err)
	}
	if decoded["is_urgent"]["type"] != "noul" {
		t.Errorf("noul type missing: %v", decoded["is_urgent"])
	}
	if decoded["department"]["type"] != "choice" || decoded["department"]["choice"] != "technical" {
		t.Errorf("choice = %v", decoded["department"])
	}
	if decoded["frustration"]["type"] != "score" {
		t.Errorf("score = %v", decoded["frustration"])
	}
	legend := mustMap(t, decoded["frustration"]["legend"])
	if legend["1"] != "Frustrated" {
		t.Errorf("legend = %v", legend)
	}
	probabilities := mustMap(t, decoded["frustration"]["probabilities"])
	if probabilities["0"] != 0.4 {
		t.Errorf("probabilities = %v", probabilities)
	}
	if decoded["future"]["ranking"] == nil {
		t.Errorf("unknown answer lost: %v", decoded["future"])
	}

	var roundTripped Answers
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatalf("round-trip Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(answers["is_urgent"], roundTripped["is_urgent"]) {
		t.Errorf("round-trip changed noul: %+v vs %+v", answers["is_urgent"], roundTripped["is_urgent"])
	}
}

func TestScoreAnswerZeroLevels(t *testing.T) {
	var answer ScoreAnswer
	if err := json.Unmarshal([]byte(`{
		"type": "score",
		"score": 0.5,
		"legend": {"0": "Never", "1": "Always"},
		"probabilities": {"0": 0.5, "1": 0.5},
		"confidence": 0.1
	}`), &answer); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if answer.Score != 0.5 || answer.Legend[0] != "Never" || answer.Probabilities[1] != 0.5 {
		t.Errorf("answer = %+v", answer)
	}
}

func TestAnswersRejectBrokenKnownAnswer(t *testing.T) {
	var answers Answers
	err := json.Unmarshal([]byte(`{"q": {"type": "choice", "choice": "a"}}`), &answers)
	if err == nil {
		t.Fatal("expected a decoding error for a choice answer without confidence")
	}
}

func TestAnswersRejectNonObject(t *testing.T) {
	var answers Answers
	if err := json.Unmarshal([]byte(`[1,2,3]`), &answers); err == nil {
		t.Fatal("expected an error for a non-object answers payload")
	}
}

func TestAnswersRejectMissingType(t *testing.T) {
	for _, payload := range []string{
		`{"q": {"noul": 0.5}}`,
		`{"q": null}`,
		`{"q": {"type": 3}}`,
	} {
		var answers Answers
		if err := json.Unmarshal([]byte(payload), &answers); err == nil {
			t.Errorf("Unmarshal(%s) succeeded, want an error", payload)
		}
	}
}

func TestRawAnswer(t *testing.T) {
	response := &SystemOneResponse{
		Raw: json.RawMessage(`{"model":"jev-latest","answers":{"is_urgent": {"type": "noul", "noul": 0.92}, "future": {"type":"ranking","ranking":["a"]}},"usage":{}}`),
	}

	got, ok := response.RawAnswer("is_urgent")
	if !ok {
		t.Fatal("RawAnswer(is_urgent) not found")
	}
	if want := `{"type": "noul", "noul": 0.92}`; string(got) != want {
		t.Errorf("RawAnswer(is_urgent) = %s, want %s", got, want)
	}

	got, ok = response.RawAnswer("future")
	if !ok {
		t.Fatal("RawAnswer(future) not found")
	}
	if want := `{"type":"ranking","ranking":["a"]}`; string(got) != want {
		t.Errorf("RawAnswer(future) = %s, want %s", got, want)
	}

	if _, ok := response.RawAnswer("missing"); ok {
		t.Error("RawAnswer(missing) found, want false")
	}
	if _, ok := (&SystemOneResponse{}).RawAnswer("is_urgent"); ok {
		t.Error("RawAnswer on a response without a raw body found an answer")
	}
	if _, ok := (&SystemOneResponse{Raw: json.RawMessage(`{`)}).RawAnswer("is_urgent"); ok {
		t.Error("RawAnswer on a malformed raw body found an answer")
	}
}

func TestSystemOneResponseMarshalOmitsRaw(t *testing.T) {
	response := &SystemOneResponse{
		Model:   "jev-latest",
		Answers: Answers{"is_urgent": NoulAnswer{Noul: 0.92}},
		Raw:     json.RawMessage(`{"answers":{"is_urgent":{"type":"noul","noul":0.92}},"extra":1e3}`),
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "extra") {
		t.Errorf("marshaled response contains raw-body fields: %s", raw)
	}
	var decoded SystemOneResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", raw, err)
	}
	if decoded.Raw != nil {
		t.Errorf("Raw = %s, want nil after a typed round trip", decoded.Raw)
	}
	if answer := decoded.Answers["is_urgent"].(NoulAnswer); answer.Noul != 0.92 {
		t.Errorf("answer = %+v", answer)
	}
}
