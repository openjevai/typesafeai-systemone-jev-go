package jev

import (
	"encoding/json"
	"fmt"
)

// Answer is the typed result of one question. Concrete types are NoulAnswer,
// ChoiceAnswer, and ScoreAnswer. Answers this SDK version does not recognize
// are preserved as UnknownAnswer, so a newer API can add answer kinds without
// breaking decoding.
type Answer interface {
	answerType() string
}

// NoulAnswer is the answer to a Noul question: the probability that the
// statement is true, from 0 to 1.
type NoulAnswer struct {
	// Noul is the probability of yes or true.
	Noul float64 `json:"noul"`
}

// ChoiceAnswer is the answer to a Choice question. Choice is always one of
// the criteria supplied in the question, never generated text.
type ChoiceAnswer struct {
	// Choice is the highest-probability option.
	Choice string `json:"choice"`
	// Probabilities maps every option to its probability; values sum to
	// approximately 1.
	Probabilities map[string]float64 `json:"probabilities"`
	// Confidence summarizes how peaked the distribution is, from 0 to 1.
	Confidence float64 `json:"confidence"`
}

// ScoreAnswer is the answer to a Score question.
type ScoreAnswer struct {
	// Score is the probability-weighted position across the levels. It can
	// land between two levels.
	Score float64 `json:"score"`
	// Legend maps each level number back to its description, exactly as it
	// was supplied in the question.
	Legend map[int]any `json:"legend"`
	// Probabilities maps each level to its probability; values sum to
	// approximately 1.
	Probabilities map[int]float64 `json:"probabilities"`
	// Confidence summarizes how peaked the distribution is, from 0 to 1.
	Confidence float64 `json:"confidence"`
}

// UnknownAnswer is an answer whose type this SDK version does not model. Raw
// holds the complete JSON object, including its type field.
type UnknownAnswer struct {
	// Type is the unrecognized answer type.
	Type string
	// Raw is the complete answer object.
	Raw json.RawMessage
}

func (NoulAnswer) answerType() string   { return TypeNoul }
func (ChoiceAnswer) answerType() string { return TypeChoice }
func (ScoreAnswer) answerType() string  { return TypeScore }
func (a UnknownAnswer) answerType() string {
	return a.Type
}

// answerFields decodes an answer object into its raw fields so required
// fields can be checked before decoding into typed values.
func answerFields(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// requireField reports a missing or null required answer field.
func requireField(fields map[string]json.RawMessage, name string) error {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return fmt.Errorf("missing required field %q", name)
	}
	return nil
}

// UnmarshalJSON decodes a noul answer, requiring its noul field.
func (a *NoulAnswer) UnmarshalJSON(data []byte) error {
	fields, err := answerFields(data)
	if err != nil {
		return err
	}
	if err := requireField(fields, "noul"); err != nil {
		return err
	}
	if err := json.Unmarshal(fields["noul"], &a.Noul); err != nil {
		return fmt.Errorf("field %q: %w", "noul", err)
	}
	return nil
}

// UnmarshalJSON decodes a choice answer, requiring its choice, probabilities,
// and confidence fields.
func (a *ChoiceAnswer) UnmarshalJSON(data []byte) error {
	fields, err := answerFields(data)
	if err != nil {
		return err
	}
	for _, name := range []string{"choice", "probabilities", "confidence"} {
		if err := requireField(fields, name); err != nil {
			return err
		}
	}
	if err := json.Unmarshal(fields["choice"], &a.Choice); err != nil {
		return fmt.Errorf("field %q: %w", "choice", err)
	}
	if err := json.Unmarshal(fields["probabilities"], &a.Probabilities); err != nil {
		return fmt.Errorf("field %q: %w", "probabilities", err)
	}
	if err := json.Unmarshal(fields["confidence"], &a.Confidence); err != nil {
		return fmt.Errorf("field %q: %w", "confidence", err)
	}
	return nil
}

// UnmarshalJSON decodes a score answer, requiring its score, legend,
// probabilities, and confidence fields.
func (a *ScoreAnswer) UnmarshalJSON(data []byte) error {
	fields, err := answerFields(data)
	if err != nil {
		return err
	}
	for _, name := range []string{"score", "legend", "probabilities", "confidence"} {
		if err := requireField(fields, name); err != nil {
			return err
		}
	}
	if err := json.Unmarshal(fields["score"], &a.Score); err != nil {
		return fmt.Errorf("field %q: %w", "score", err)
	}
	if err := json.Unmarshal(fields["legend"], &a.Legend); err != nil {
		return fmt.Errorf("field %q: %w", "legend", err)
	}
	if err := json.Unmarshal(fields["probabilities"], &a.Probabilities); err != nil {
		return fmt.Errorf("field %q: %w", "probabilities", err)
	}
	if err := json.Unmarshal(fields["confidence"], &a.Confidence); err != nil {
		return fmt.Errorf("field %q: %w", "confidence", err)
	}
	return nil
}

// MarshalJSON writes the answer with its type discriminator.
func (a NoulAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type string  `json:"type"`
		Noul float64 `json:"noul"`
	}{Type: TypeNoul, Noul: a.Noul})
}

// MarshalJSON writes the answer with its type discriminator.
func (a ChoiceAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type          string             `json:"type"`
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	}{Type: TypeChoice, Choice: a.Choice, Probabilities: a.Probabilities, Confidence: a.Confidence})
}

// MarshalJSON writes the answer with its type discriminator.
func (a ScoreAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type          string          `json:"type"`
		Score         float64         `json:"score"`
		Legend        map[int]any     `json:"legend"`
		Probabilities map[int]float64 `json:"probabilities"`
		Confidence    float64         `json:"confidence"`
	}{Type: TypeScore, Score: a.Score, Legend: a.Legend, Probabilities: a.Probabilities, Confidence: a.Confidence})
}

// MarshalJSON returns the preserved raw answer.
func (a UnknownAnswer) MarshalJSON() ([]byte, error) {
	if len(a.Raw) == 0 {
		return []byte("null"), nil
	}
	return a.Raw, nil
}

// Answers maps question IDs to their answers. Use the SystemOneResponse
// accessors (Noul, Choice, Score) for type-safe reads.
type Answers map[string]Answer

// UnmarshalJSON decodes each answer by its type discriminator.
func (a *Answers) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	answers := make(Answers, len(raw))
	for name, item := range raw {
		var discriminator struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item, &discriminator); err != nil {
			return fmt.Errorf("answer %q: %w", name, err)
		}
		if discriminator.Type == "" {
			return fmt.Errorf("answer %q: missing required field %q", name, "type")
		}
		switch discriminator.Type {
		case TypeNoul:
			var answer NoulAnswer
			if err := json.Unmarshal(item, &answer); err != nil {
				return fmt.Errorf("answer %q: %w", name, err)
			}
			answers[name] = answer
		case TypeChoice:
			var answer ChoiceAnswer
			if err := json.Unmarshal(item, &answer); err != nil {
				return fmt.Errorf("answer %q: %w", name, err)
			}
			answers[name] = answer
		case TypeScore:
			var answer ScoreAnswer
			if err := json.Unmarshal(item, &answer); err != nil {
				return fmt.Errorf("answer %q: %w", name, err)
			}
			answers[name] = answer
		default:
			answers[name] = UnknownAnswer{Type: discriminator.Type, Raw: item}
		}
	}
	*a = answers
	return nil
}

// MarshalJSON writes each answer with its type discriminator.
func (a Answers) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]Answer(a))
}

// Usage reports the token counts for a request.
type Usage struct {
	// InputTokens is the number of billable input tokens. Output tokens are
	// currently free of charge.
	InputTokens int64 `json:"input_tokens"`
	// OutputTokens is the number of tokens used to answer the questions.
	OutputTokens int64 `json:"output_tokens"`
}

// SystemOneResponse holds one answer per question, keyed by the IDs supplied
// in the request, plus the model that answered and its token usage.
type SystemOneResponse struct {
	// Model is the model that performed the evaluation. It reports the
	// versioned ID that answered, which may differ from the alias sent.
	Model string `json:"model"`
	// Answers holds every answer, including UnknownAnswer values for
	// unrecognized answer kinds.
	Answers Answers `json:"answers"`
	// Usage is the token usage for the request.
	Usage Usage `json:"usage"`
	// RequestID is the x-typesafe-request-id response header. Quote it in
	// support requests.
	RequestID string `json:"-"`
}

// Answer returns the answer stored under name.
func (r *SystemOneResponse) Answer(name string) (Answer, bool) {
	answer, ok := r.Answers[name]
	return answer, ok
}

// Noul returns the Noul answer stored under name.
func (r *SystemOneResponse) Noul(name string) (NoulAnswer, bool) {
	answer, ok := r.Answers[name].(NoulAnswer)
	return answer, ok
}

// Choice returns the Choice answer stored under name.
func (r *SystemOneResponse) Choice(name string) (ChoiceAnswer, bool) {
	answer, ok := r.Answers[name].(ChoiceAnswer)
	return answer, ok
}

// Score returns the Score answer stored under name.
func (r *SystemOneResponse) Score(name string) (ScoreAnswer, bool) {
	answer, ok := r.Answers[name].(ScoreAnswer)
	return answer, ok
}

// Nouls returns every Noul answer, keyed by question ID.
func (r *SystemOneResponse) Nouls() map[string]NoulAnswer {
	answers := make(map[string]NoulAnswer)
	for name, answer := range r.Answers {
		if typed, ok := answer.(NoulAnswer); ok {
			answers[name] = typed
		}
	}
	return answers
}

// Choices returns every Choice answer, keyed by question ID.
func (r *SystemOneResponse) Choices() map[string]ChoiceAnswer {
	answers := make(map[string]ChoiceAnswer)
	for name, answer := range r.Answers {
		if typed, ok := answer.(ChoiceAnswer); ok {
			answers[name] = typed
		}
	}
	return answers
}

// Scores returns every Score answer, keyed by question ID.
func (r *SystemOneResponse) Scores() map[string]ScoreAnswer {
	answers := make(map[string]ScoreAnswer)
	for name, answer := range r.Answers {
		if typed, ok := answer.(ScoreAnswer); ok {
			answers[name] = typed
		}
	}
	return answers
}
