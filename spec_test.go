package jev

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The tests in this file check the SDK types against the vendored OpenAPI
// specification, using the examples embedded in the schema.

func loadOpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("openapi/typesafe-openapi.json")
	if err != nil {
		t.Fatalf("read vendored spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode vendored spec: %v", err)
	}
	return spec
}

// specSchema returns a named schema from components.schemas.
func specSchema(t *testing.T, spec map[string]any, name string) map[string]any {
	t.Helper()
	schemas := mustMap(t, mustMap(t, spec["components"])["schemas"])
	schema, ok := schemas[name]
	if !ok {
		t.Fatalf("schema %q not found in vendored spec", name)
	}
	return mustMap(t, schema)
}

// specExample returns the first example of a schema property.
func specExample(t *testing.T, schema map[string]any, property string) any {
	t.Helper()
	properties := mustMap(t, schema["properties"])
	prop, ok := properties[property]
	if !ok {
		t.Fatalf("property %q not found in schema %v", property, schema["title"])
	}
	examples := mustSlice(t, mustMap(t, prop)["examples"])
	if len(examples) == 0 {
		t.Fatalf("property %q has no examples", property)
	}
	return examples[0]
}

func specRequired(t *testing.T, schema map[string]any) []string {
	t.Helper()
	raw := mustSlice(t, schema["required"])
	names := make([]string, 0, len(raw))
	for _, name := range raw {
		names = append(names, name.(string))
	}
	return names
}

func TestSpecPathsMatchClient(t *testing.T) {
	spec := loadOpenAPISpec(t)
	paths := mustMap(t, spec["paths"])
	if _, ok := paths[pathSystemOne]; !ok {
		t.Errorf("vendored spec is missing path %q", pathSystemOne)
	}
	if _, ok := paths[pathModels]; !ok {
		t.Errorf("vendored spec is missing path %q", pathModels)
	}
	systemOne := mustMap(t, mustMap(t, paths[pathSystemOne])["post"])
	if _, ok := systemOne["requestBody"]; !ok {
		t.Error("POST /v1/systemone should have a request body")
	}
}

func TestSpecQuestionDiscriminatorsMatch(t *testing.T) {
	spec := loadOpenAPISpec(t)
	question := specSchema(t, spec, "Question")
	mapping := mustMap(t, mustMap(t, question["discriminator"])["mapping"])
	got := map[string]bool{}
	for kind := range mapping {
		got[kind] = true
	}
	want := map[string]bool{TypeNoul: true, TypeChoice: true, TypeScore: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spec question types = %v, want %v", got, want)
	}
}

func TestSpecNoulQuestionExampleRoundTrip(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "NoulQuestion")
	instructions := specExample(t, schema, "instructions")
	criteria := mustMap(t, specExample(t, schema, "criteria"))

	question := Noul{
		Instructions: instructions,
		Criteria: &NoulCriteria{
			True:  criteria["true"],
			False: criteria["false"],
		},
	}
	want := map[string]any{
		"type":         TypeNoul,
		"instructions": instructions,
		"criteria":     criteria,
	}
	if got := marshalToAny(t, question); !reflect.DeepEqual(got, want) {
		t.Errorf("noul question = %v, want %v", got, want)
	}
}

func TestSpecChoiceQuestionExampleRoundTrip(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "ChoiceQuestion")
	instructions := specExample(t, schema, "instructions")
	criteria := mustMap(t, specExample(t, schema, "criteria"))

	choices := Choices{}
	for name, description := range criteria {
		choices[name] = description
	}
	question := Choice{Instructions: instructions, Criteria: choices}
	want := map[string]any{
		"type":         TypeChoice,
		"instructions": instructions,
		"criteria":     criteria,
	}
	if got := marshalToAny(t, question); !reflect.DeepEqual(got, want) {
		t.Errorf("choice question = %v, want %v", got, want)
	}
}

func TestSpecScoreQuestionExampleRoundTrip(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "ScoreQuestion")
	instructions := specExample(t, schema, "instructions")
	criteria := mustSlice(t, specExample(t, schema, "criteria"))

	question := Score{Instructions: instructions, Criteria: criteria}
	want := map[string]any{
		"type":         TypeScore,
		"instructions": instructions,
		"criteria":     criteria,
	}
	if got := marshalToAny(t, question); !reflect.DeepEqual(got, want) {
		t.Errorf("score question = %v, want %v", got, want)
	}
}

func TestSpecAnswersConformToSchema(t *testing.T) {
	spec := loadOpenAPISpec(t)
	tests := []struct {
		schema   string
		answer   Answer
		target   any
		required []string
	}{
		{
			schema:   "NoulAnswer",
			answer:   NoulAnswer{Noul: 0.98},
			target:   &NoulAnswer{},
			required: []string{"noul", "type"},
		},
		{
			schema: "ChoiceAnswer",
			answer: ChoiceAnswer{
				Choice:        "billing",
				Probabilities: map[string]float64{"billing": 0.9, "technical": 0.1},
				Confidence:    0.9,
			},
			target:   &ChoiceAnswer{},
			required: []string{"choice", "confidence", "probabilities", "type"},
		},
		{
			schema: "ScoreAnswer",
			answer: ScoreAnswer{
				Score:         1.7,
				Legend:        map[int]any{0: "Can wait", 1: "Needs attention"},
				Probabilities: map[int]float64{0: 0.1, 1: 0.9},
				Confidence:    0.9,
			},
			target:   &ScoreAnswer{},
			required: []string{"score", "confidence", "legend", "probabilities", "type"},
		},
	}
	for _, test := range tests {
		t.Run(test.schema, func(t *testing.T) {
			schema := specSchema(t, spec, test.schema)
			got := marshalToAny(t, test.answer)
			for _, field := range specRequired(t, schema) {
				if _, ok := got[field]; !ok {
					t.Errorf("marshaled %s is missing required field %q: %v", test.schema, field, got)
				}
			}

			// Assemble a whole answer from the property examples in the
			// spec, decode it through the SDK, and check it round-trips.
			example := map[string]any{}
			for field := range mustMap(t, schema["properties"]) {
				example[field] = specExample(t, schema, field)
			}
			raw, err := json.Marshal(example)
			if err != nil {
				t.Fatalf("marshal example: %v", err)
			}
			var answers Answers
			if err := json.Unmarshal([]byte(`{"q":`+string(raw)+`}`), &answers); err != nil {
				t.Fatalf("decode example %s: %v", raw, err)
			}
			if !reflect.DeepEqual(marshalToAny(t, answers["q"]), example) {
				t.Errorf("round-trip changed %s: %v vs %v", test.schema, answers["q"], example)
			}
		})
	}
}

func TestSpecSystemOneRequestRequiredFields(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "SystemOneRequest")
	request := SystemOneRequest{
		State:     specExample(t, schema, "state"),
		Model:     specExample(t, schema, "model").(string),
		Questions: Questions{"billing": Noul{Instructions: specExample(t, specSchema(t, spec, "NoulQuestion"), "instructions")}},
	}
	got := marshalToAny(t, request)
	for _, field := range specRequired(t, schema) {
		if _, ok := got[field]; !ok {
			t.Errorf("request is missing required field %q: %v", field, got)
		}
	}
}

func TestSpecSystemOneResponseExampleDecodes(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "SystemOneResponse")
	answersExample := mustMap(t, specExample(t, schema, "answers"))
	usageExample := mustMap(t, specExample(t, schema, "usage"))
	modelExample := specExample(t, schema, "model").(string)

	payload, err := json.Marshal(map[string]any{
		"model":   modelExample,
		"answers": answersExample,
		"usage":   usageExample,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var response SystemOneResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("Unmarshal(%s): %v", payload, err)
	}
	if response.Model != modelExample {
		t.Errorf("Model = %q, want %q", response.Model, modelExample)
	}
	if len(response.Answers) != len(answersExample) {
		t.Errorf("answers = %v, want %d entries", response.Answers, len(answersExample))
	}
	if response.Usage.InputTokens != int64(usageExample["input_tokens"].(float64)) {
		t.Errorf("InputTokens = %d", response.Usage.InputTokens)
	}

	// Every answer example re-marshals to the same JSON object.
	for name, raw := range answersExample {
		answer, ok := response.Answers[name]
		if !ok {
			t.Fatalf("answer %q missing", name)
		}
		got := marshalToAny(t, answer)
		if !reflect.DeepEqual(got, raw) {
			t.Errorf("answer %q = %v, want %v", name, got, raw)
		}
	}
}

func TestSpecModelsExampleDecodes(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "ModelMetadataList")
	models := mustSlice(t, specExample(t, schema, "models"))

	payload, err := json.Marshal(map[string]any{"models": models})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var response ModelsResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("Unmarshal(%s): %v", payload, err)
	}
	if len(response.Models) != len(models) {
		t.Fatalf("models = %+v, want %d entries", response.Models, len(models))
	}
	for index, model := range response.Models {
		want := mustMap(t, models[index])
		if model.Name != want["name"] || model.Description != want["description"] || model.ReleaseDate != want["release_date"] {
			t.Errorf("model %d = %+v, want %v", index, model, want)
		}
	}
}

func TestSpecValidationErrorMessage(t *testing.T) {
	spec := loadOpenAPISpec(t)
	schema := specSchema(t, spec, "HTTPValidationError")
	detail := mustSlice(t, specExample(t, schema, "detail"))

	payload, err := json.Marshal(map[string]any{"detail": detail})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if message := extractErrorMessage(payload); message != "state: Field required" {
		t.Errorf("message = %q, want %q", message, "state: Field required")
	}
}
