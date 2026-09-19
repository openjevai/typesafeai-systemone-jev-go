package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// Question is one typed judgment about the state. The three kinds are Noul,
// Choice, and Score. Questions from this package are the intended way to ask;
// RawQuestion is the escape hatch for fields and question kinds newer than
// this SDK.
//
// Question IDs are chosen by your code, are not sent to the model, and are
// echoed back as the keys of Answers. Put the full question in Instructions.
type Question interface {
	questionType() string
	validate() error
}

// Question type discriminators.
const (
	TypeNoul   = "noul"
	TypeChoice = "choice"
	TypeScore  = "score"
)

// Questions maps question IDs, chosen by your code, to questions. At least one
// question is required, and every question in a request sees the same state.
type Questions map[string]Question

// Noul asks for the probability that a yes/no statement is true. The answer
// is a number from 0 (no) to 1 (yes); near 0.5 means the model gives both
// outcomes similar probability. Use Noul when the probability itself is the
// useful signal, and a Score when you need a position on a spectrum.
type Noul struct {
	// Instructions is the yes/no question to evaluate, as a string, or as a
	// map[string]any or []any for a structured description.
	Instructions any
	// Criteria optionally describes what counts as yes and no.
	Criteria *NoulCriteria
}

// NoulCriteria describes the two outcomes of a Noul question. Either side may
// be nil, and each may be a string, map[string]any, or []any.
type NoulCriteria struct {
	// True describes what a yes answer (value near 1) means.
	True any `json:"true,omitempty"`
	// False describes what a no answer (value near 0) means.
	False any `json:"false,omitempty"`
}

// Choice asks the model to select one option from Criteria. The answer names
// the highest-probability option and carries the full distribution, so code
// can act on the choice and flag low-confidence selections.
type Choice struct {
	// Instructions asks what the model should decide.
	Instructions any
	// Criteria maps each option to a description of when it applies. A nil
	// description lets the option name stand alone.
	Criteria Choices
}

// Choices maps choice options to descriptions of when each applies. Values
// may be a string, map[string]any, []any, or nil.
type Choices map[string]any

// Score asks the model to rate the state along ordered levels. The answer is
// the probability-weighted position, which may land between levels.
type Score struct {
	// Instructions asks what the model should rate.
	Instructions any
	// Criteria is the ordered list of level descriptions. The position in
	// the list determines the score, starting at zero. Two or more levels
	// give the most useful answers.
	Criteria []any
}

// RawQuestion sends a question verbatim, for fields and question kinds newer
// than this SDK understands. The map must contain a non-empty "type" and, for
// choice and score questions, a "criteria" entry.
//
//	jev.RawQuestion{
//		"type":         "choice",
//		"instructions": "Which language is this?",
//		"criteria":     map[string]any{"go": nil, "rust": nil, "other": nil},
//	}
type RawQuestion map[string]any

// MarshalJSON writes the question with its type discriminator.
func (q Noul) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string        `json:"type"`
		Instructions any           `json:"instructions,omitempty"`
		Criteria     *NoulCriteria `json:"criteria,omitempty"`
	}{Type: TypeNoul, Instructions: q.Instructions, Criteria: q.Criteria})
}

// MarshalJSON writes the question with its type discriminator.
func (q Choice) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string  `json:"type"`
		Instructions any     `json:"instructions,omitempty"`
		Criteria     Choices `json:"criteria"`
	}{Type: TypeChoice, Instructions: q.Instructions, Criteria: q.Criteria})
}

// MarshalJSON writes the question with its type discriminator.
func (q Score) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string `json:"type"`
		Instructions any    `json:"instructions,omitempty"`
		Criteria     []any  `json:"criteria"`
	}{Type: TypeScore, Instructions: q.Instructions, Criteria: q.Criteria})
}

func (Noul) questionType() string { return TypeNoul }
func (Choice) questionType() string {
	return TypeChoice
}
func (Score) questionType() string { return TypeScore }
func (q RawQuestion) questionType() string {
	if q == nil {
		return ""
	}
	kind, _ := q["type"].(string)
	return kind
}

func (Noul) validate() error { return nil }

func (q Choice) validate() error {
	if len(q.Criteria) == 0 {
		return errors.New("a choice question requires at least one criterion")
	}
	return nil
}

func (q Score) validate() error {
	if len(q.Criteria) == 0 {
		return errors.New("a score question requires at least one criterion")
	}
	return nil
}

func (q RawQuestion) validate() error {
	kind := q.questionType()
	if kind == "" {
		return errors.New(`a raw question requires a non-empty "type" field`)
	}
	if kind == TypeChoice || kind == TypeScore {
		if _, ok := q["criteria"]; !ok {
			return fmt.Errorf("a %s question requires criteria", kind)
		}
		if kind == TypeScore {
			if levels, ok := q["criteria"].([]any); ok && len(levels) == 0 {
				return errors.New("a score question requires at least one criterion")
			}
		}
	}
	return nil
}

// validateQuestion rejects nil questions before they reach the wire.
func validateQuestion(q Question) error {
	if q == nil {
		return errors.New("question must not be nil")
	}
	value := reflect.ValueOf(q)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return errors.New("question must not be nil")
		}
	}
	return q.validate()
}
