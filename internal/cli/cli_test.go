package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jev "github.com/mheers/typesafeai-systemone-jev-go"
)

// runCLI runs the command line against a test server and captures its output.
func runCLI(t *testing.T, args []string, stdin string, handler http.HandlerFunc) (int, string, string) {
	t.Helper()
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv(jev.EnvAPIKey, "test-key")
	t.Setenv(jev.EnvBaseURL, server.URL)
	t.Setenv(jev.EnvModel, "")
	t.Setenv(jev.EnvLogLevel, "")

	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

const modelsBody = `{"models":[
	{"name":"jev-latest","description":"General-purpose system one model.","release_date":"2026-09-15"},
	{"name":"jev-preview","description":"Preview alias.","release_date":"2026-09-15"}
]}`

func TestModelsCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"models"}, "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(modelsBody))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "jev-latest") || !strings.Contains(stdout, "2026-09-15") {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "{") {
		t.Errorf("default output should be a table, got %q", stdout)
	}
}

func TestModelsCommandJSON(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"models", "--json"}, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(modelsBody))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	var decoded struct {
		Models []jev.ModelMetadata `json:"models"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(decoded.Models) != 2 || decoded.Models[0].Name != "jev-latest" {
		t.Errorf("models = %+v", decoded.Models)
	}
}

const triageAnswers = `{
	"model": "jev-latest",
	"answers": {
		"department": {
			"type": "choice",
			"choice": "billing",
			"probabilities": {"billing": 0.84, "technical": 0.16},
			"confidence": 0.68
		},
		"is_urgent": {"type": "noul", "noul": 0.99},
		"frustration": {
			"type": "score",
			"score": 1.6,
			"legend": {"0": "Calm", "1": "Frustrated", "2": "Very angry"},
			"probabilities": {"0": 0.05, "1": 0.3, "2": 0.65},
			"confidence": 0.78
		}
	},
	"usage": {"input_tokens": 312, "output_tokens": 48}
}`

func TestAskCommand(t *testing.T) {
	questions := writeTempFile(t, "questions.json", `{
		"department": {"type": "choice", "instructions": "Which team?", "criteria": {"billing": null, "technical": null}},
		"is_urgent": {"type": "noul", "instructions": "Is it urgent?"}
	}`)
	code, stdout, stderr := runCLI(t, []string{"ask", "--questions", questions, "--state", "Stripe connection failing"}, "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(triageAnswers))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	for _, want := range []string{"department", "billing", "confidence", "is_urgent", "0.99", "frustration", "1.6"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
}

func TestAskCommandJSON(t *testing.T) {
	questions := writeTempFile(t, "questions.json", `{"is_urgent": {"type": "noul", "instructions": "Is it urgent?"}}`)
	code, stdout, stderr := runCLI(t, []string{"ask", "--json", "--questions", questions, "--state", "help"}, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(triageAnswers))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	var response jev.SystemOneResponse
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("stdout is not a response: %v\n%s", err, stdout)
	}
	if answer, ok := response.Noul("is_urgent"); !ok || answer.Noul != 0.99 {
		t.Errorf("answer = %+v, %v", answer, ok)
	}
}

func TestAskStateFromStdin(t *testing.T) {
	questions := writeTempFile(t, "questions.json", `{"is_urgent": {"type": "noul", "instructions": "Is it urgent?"}}`)
	var sawState string
	code, _, stderr := runCLI(t, []string{"ask", "--questions", questions}, "piped state\n", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sawState = body.State
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(triageAnswers))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if sawState != "piped state" {
		t.Errorf("state = %q, want trimmed stdin", sawState)
	}
}

func TestAskQuestionsFromStdin(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"ask", "--questions", "-", "--state", "hello"},
		`{"is_urgent": {"type": "noul", "instructions": "Is it urgent?"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(triageAnswers))
		})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
}

func TestNoulCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{
		"noul",
		"--state", "Wire transfer failed, please help",
		"--instructions", "Does this message convey urgency?",
		"--true", "Time-sensitive",
	}, "", func(w http.ResponseWriter, r *http.Request) {
		body, _ := decodeRequestBody(t, r)
		questions := body["questions"].(map[string]any)
		question := questions["answer"].(map[string]any)
		if question["type"] != "noul" || question["instructions"] != "Does this message convey urgency?" {
			t.Errorf("question = %v", question)
		}
		criteria := question["criteria"].(map[string]any)
		if criteria["true"] != "Time-sensitive" {
			t.Errorf("criteria = %v", criteria)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"answer":{"type":"noul","noul":0.98}},"usage":{}}`))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "answer") || !strings.Contains(stdout, "0.98") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestChoiceCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{
		"choice",
		"--state", "The API returns 500s",
		"--instructions", "Which team should handle this?",
		"--option", "billing=Payments",
		"--option", "technical",
	}, "", func(w http.ResponseWriter, r *http.Request) {
		body, _ := decodeRequestBody(t, r)
		question := body["questions"].(map[string]any)["answer"].(map[string]any)
		criteria := question["criteria"].(map[string]any)
		if criteria["billing"] != "Payments" || criteria["technical"] != nil {
			t.Errorf("criteria = %v", criteria)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"answer":{"type":"choice","choice":"technical","probabilities":{"billing":0.1,"technical":0.9},"confidence":0.8}},"usage":{}}`))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "technical") || !strings.Contains(stdout, "0.9") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestChoiceCommandEmptyOption(t *testing.T) {
	code, _, stderr := runCLI(t, []string{
		"choice", "--state", "x", "--instructions", "?", "--option", "=description",
	}, "", nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr %s)", code, stderr)
	}
	if !strings.Contains(stderr, "--option") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestScoreCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{
		"score",
		"--state", "The delivery is three days late.",
		"--instructions", "How frustrated is the customer?",
		"--level", "Calm",
		"--level", "Frustrated",
		"--level", "Very angry",
	}, "", func(w http.ResponseWriter, r *http.Request) {
		body, _ := decodeRequestBody(t, r)
		question := body["questions"].(map[string]any)["answer"].(map[string]any)
		criteria := question["criteria"].([]any)
		if len(criteria) != 3 || criteria[0] != "Calm" {
			t.Errorf("criteria = %v", criteria)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"answer":{"type":"score","score":1.5,"legend":{"0":"Calm","1":"Frustrated","2":"Very angry"},"probabilities":{"0":0.1,"1":0.5,"2":0.4},"confidence":0.5}},"usage":{}}`))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "score") || !strings.Contains(stdout, "1.5") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestBatchCommand(t *testing.T) {
	batch := writeTempFile(t, "batch.json", `[
		{"state": "request-0", "questions": {"q": {"type": "noul", "instructions": "Is this hello?"}}},
		{"state": "request-1", "questions": {"q": {"type": "noul", "instructions": "Is this hello?"}}},
		{"state": "request-2", "questions": {"q": {"type": "noul", "instructions": "Is this hello?"}}}
	]`)
	code, stdout, stderr := runCLI(t, []string{"batch", "--file", batch, "--concurrency", "2", "--ordered"}, "",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				State string `json:"state"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.State == "request-1" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`))
		})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr %s)", code, stderr)
	}
	if !strings.Contains(stderr, "1 of 3 requests failed") {
		t.Errorf("stderr = %q", stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3\n%s", len(lines), stdout)
	}
	for index, line := range lines {
		var entry struct {
			Index    int             `json:"index"`
			Response json.RawMessage `json:"response"`
			Error    string          `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d is not JSON: %v", index, err)
		}
		if entry.Index != index {
			t.Errorf("line %d has index %d; --ordered should preserve order", index, entry.Index)
		}
		if index == 1 {
			if entry.Error == "" {
				t.Errorf("line 1 should carry an error")
			}
		} else if len(entry.Response) == 0 {
			t.Errorf("line %d should carry a response", index)
		}
	}
}

func TestBatchCommandThroughStdin(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"batch", "--file", "-"}, `[
		{"state": "hello", "questions": {"q": {"type": "noul", "instructions": "Is this hello?"}}}
	]`, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, `"response"`) {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	tests := map[string]struct {
		args  []string
		stdin string
		want  string
	}{
		"missing state": {
			args: []string{"noul", "--instructions", "Is this hello?"},
			want: "no state",
		},
		"missing instructions": {
			args: []string{"noul", "--state", "hello"},
			want: "--instructions is required",
		},
		"missing questions": {
			args: []string{"ask", "--state", "hello"},
			want: "--questions is required",
		},
		"missing batch file": {
			args: []string{"batch"},
			want: "--file is required",
		},
		"state and state file": {
			args: []string{"noul", "--state", "hello", "--state-file", "x", "--instructions", "?"},
			want: "not both",
		},
		"unexpected argument": {
			args: []string{"models", "extra"},
			want: "unexpected argument",
		},
		"unknown command": {
			args: []string{"frobnicate"},
			want: "unknown command",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := runCLI(t, test.args, test.stdin, nil)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2 (stderr %s)", code, stderr)
			}
			if !strings.Contains(stderr, test.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, test.want)
			}
		})
	}
}

func TestFlagParseError(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"models", "--nope"}, "", nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "nope") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestHelpAndVersion(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"help"}, "", nil)
	if code != 0 {
		t.Fatalf("help exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "batch") {
		t.Errorf("help output = %q", stdout)
	}

	code, stdout, stderr = runCLI(t, []string{"version"}, "", nil)
	if code != 0 {
		t.Fatalf("version exit code = %d, stderr = %s", code, stderr)
	}
	if want := "jev " + jev.Version + "\n"; stdout != want {
		t.Errorf("version output = %q, want %q", stdout, want)
	}
}

func TestAPIErrorExitCode(t *testing.T) {
	code, _, stderr := runCLI(t, []string{
		"noul", "--state", "hello", "--instructions", "Is this hello?",
	}, "", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Invalid API key"}`))
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Invalid API key") {
		t.Errorf("stderr = %q", stderr)
	}
}

// decodeRequestBody decodes a JSON request body into a generic map.
func decodeRequestBody(t *testing.T, r *http.Request) (map[string]any, error) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

// rawAskBody is deliberately formatted so the CLI test can tell the verbatim
// response apart from a re-marshaled one.
const rawAskBody = `{
  "model": "jev-latest",
  "answers": {
    "is_urgent": {"type": "noul", "noul": 0.99, "note": "kept verbatim"}
  },
  "usage": {"input_tokens": 12, "output_tokens": 3},
  "extra": 1e3
}`

func TestAskCommandJSONPrintsRawResponse(t *testing.T) {
	questions := writeTempFile(t, "questions.json", `{"is_urgent": {"type": "noul", "instructions": "Is it urgent?"}}`)
	code, stdout, stderr := runCLI(t, []string{"ask", "--json", "--questions", questions, "--state", "help"}, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rawAskBody))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if want := rawAskBody + "\n"; stdout != want {
		t.Errorf("stdout changed:\ngot  %s\nwant %s", stdout, want)
	}
}

func TestModelsCommandJSONPrintsRawResponse(t *testing.T) {
	const body = `{
  "models": [{"name": "jev-latest", "description": "General-purpose.", "release_date": "2026-09-15"}],
  "extra": 1e3
}`
	code, stdout, stderr := runCLI(t, []string{"models", "--json"}, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if want := body + "\n"; stdout != want {
		t.Errorf("stdout changed:\ngot  %s\nwant %s", stdout, want)
	}
}
