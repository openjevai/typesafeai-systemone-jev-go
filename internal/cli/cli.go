// Package cli implements the jev command line. It is internal so the command
// surface can change; the public API is the jev package.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	jev "github.com/mheers/typesafeai-systemone-jev-go"
)

const usageText = `jev - TypeSafe System One from the command line

Usage:
  jev <command> [flags]

Commands:
  models   List the models available to the account
  ask      Ask a JSON set of questions about state
  noul     Ask one yes/no question
  choice   Ask one choice question
  score    Ask one score question
  batch    Stream a JSON array of requests, one JSON result per line
  help     Show this help
  version  Show the SDK version

Configuration:
  TYPESAFE_API_KEY        API key (required)
  TYPESAFE_BASE_URL       API root (default https://api.typesafe.ai)
  TYPESAFE_DEFAULT_MODEL  Model for requests that do not name one

Examples:
  jev models
  jev ask --questions questions.json --state "My invoice is wrong"
  echo "The delivery is three days late." | jev score \
      --instructions "How frustrated is the customer?" \
      --level Calm --level Frustrated --level "Very angry"
  jev noul --state "Wire transfer failed, please help" \
      --instructions "Does this message convey urgency?"
  jev batch --file requests.json --concurrency 4

Run 'jev <command> -h' for the flags of a command.
`

// Main runs the jev command line and returns the process exit code: 0 on
// success, 1 on a runtime failure, and 2 on a usage problem.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usageText)
		return 2
	}
	err := dispatch(args[0], args[1:], stdin, stdout, stderr)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	}
	var flagErr *flagParseError
	if errors.As(err, &flagErr) {
		return 2 // the flag package already printed the problem
	}
	var usageErr *usageError
	if errors.As(err, &usageErr) {
		_, _ = fmt.Fprintf(stderr, "jev: %v\n\nRun 'jev help' for usage.\n", usageErr)
		return 2
	}
	_, _ = fmt.Fprintf(stderr, "jev: %v\n", err)
	return 1
}

// dispatch routes a command to its implementation.
func dispatch(command string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	switch command {
	case "models":
		return runModels(args, stdout, stderr)
	case "ask":
		return runAsk(args, stdin, stdout, stderr)
	case "noul":
		return runNoul(args, stdin, stdout, stderr)
	case "choice":
		return runChoice(args, stdin, stdout, stderr)
	case "score":
		return runScore(args, stdin, stdout, stderr)
	case "batch":
		return runBatch(args, stdin, stdout, stderr)
	case "help", "-h", "--help":
		_, err := fmt.Fprint(stdout, usageText)
		return err
	case "version", "-v", "--version":
		_, err := fmt.Fprintf(stdout, "jev %s\n", jev.Version)
		return err
	default:
		return usagef("unknown command %q", command)
	}
}

// usageError reports a problem with how the command was invoked.
type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

func usagef(format string, args ...any) error {
	return &usageError{message: fmt.Sprintf(format, args...)}
}

// flagParseError reports that the flag package already printed a parse error.
type flagParseError struct{}

func (*flagParseError) Error() string { return "invalid flags" }

// newFlagSet builds a flag set that reports problems to stderr.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("jev "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parse parses flags and rejects leftover positional arguments.
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &flagParseError{}
	}
	if fs.NArg() > 0 {
		return usagef("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

// clientOptions are the flags shared by every command that calls the API.
type clientOptions struct {
	model   string
	timeout time.Duration
	jsonOut bool
}

// registerConnection adds the model and timeout flags.
func (o *clientOptions) registerConnection(fs *flag.FlagSet) {
	fs.StringVar(&o.model, "model", "", "model to use (default: TYPESAFE_DEFAULT_MODEL or "+jev.DefaultModel+")")
	fs.DurationVar(&o.timeout, "timeout", jev.DefaultTimeout, "per-attempt request timeout")
}

// register adds the connection flags and the JSON output flag.
func (o *clientOptions) register(fs *flag.FlagSet) {
	o.registerConnection(fs)
	fs.BoolVar(&o.jsonOut, "json", false, "print the raw JSON response")
}

// client builds the SDK client from the flags and environment.
func (o *clientOptions) client() (*jev.Client, error) {
	var opts []jev.Option
	if o.model != "" {
		opts = append(opts, jev.WithModel(o.model))
	}
	if o.timeout > 0 {
		opts = append(opts, jev.WithTimeout(o.timeout))
	}
	client, err := jev.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// stateFlags are the flags that supply the state to evaluate.
type stateFlags struct {
	text   string
	file   string
	asJSON bool
}

func (s *stateFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&s.text, "state", "", "state to evaluate: text, or JSON when --state-json is set")
	fs.StringVar(&s.file, "state-file", "", "read the state from a file, or - for stdin")
	fs.BoolVar(&s.asJSON, "state-json", false, "parse the state as JSON (object, array, or string)")
}

// needsStdin reports whether the state will be read from standard input.
func (s *stateFlags) needsStdin() bool {
	return s.text == "" && (s.file == "" || s.file == "-")
}

// read resolves the state from the flags or stdin.
func (s *stateFlags) read(stdin io.Reader) (any, error) {
	if s.text != "" && s.file != "" {
		return nil, usagef("use --state or --state-file, not both")
	}
	var text string
	switch {
	case s.file != "":
		data, err := readResource(s.file, stdin)
		if err != nil {
			return nil, err
		}
		text = string(data)
	case s.text != "":
		text = s.text
	default:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		text = string(data)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, usagef("no state: pass --state, --state-file, or pipe text on stdin")
	}
	if !s.asJSON {
		return text, nil
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil, usagef("state is not valid JSON: %v", err)
	}
	return value, nil
}

// readResource reads a file, or stdin when path is "-".
func readResource(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// runModels lists the available models.
func runModels(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("models", stderr)
	var options clientOptions
	options.register(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	client, err := options.client()
	if err != nil {
		return err
	}
	response, err := client.ListModels(context.Background())
	if err != nil {
		return err
	}
	if options.jsonOut {
		return writeJSON(stdout, response)
	}
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, model := range response.Models {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\n", model.Name, model.ReleaseDate, model.Description)
	}
	return table.Flush()
}

// runAsk asks a JSON set of questions about the state.
func runAsk(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("ask", stderr)
	var options clientOptions
	options.register(fs)
	var state stateFlags
	state.register(fs)
	var questionsPath string
	fs.StringVar(&questionsPath, "questions", "", "path to a JSON object of questions, or - for stdin (required)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if questionsPath == "" {
		return usagef("--questions is required")
	}
	if questionsPath == "-" && state.needsStdin() {
		return usagef("--questions - reads stdin, so provide the state with --state or --state-file")
	}
	questions, err := readQuestions(questionsPath, stdin)
	if err != nil {
		return err
	}
	stateValue, err := state.read(stdin)
	if err != nil {
		return err
	}
	client, err := options.client()
	if err != nil {
		return err
	}
	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State:     stateValue,
		Questions: questions,
	})
	if err != nil {
		return err
	}
	return writeResponse(stdout, response, options.jsonOut)
}

// readQuestions reads a JSON object of question IDs to questions, in the wire
// shape the System One API accepts.
func readQuestions(path string, stdin io.Reader) (jev.Questions, error) {
	data, err := readResource(path, stdin)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, usagef("questions are not a JSON object: %v", err)
	}
	if len(raw) == 0 {
		return nil, usagef("questions must contain at least one entry")
	}
	questions := make(jev.Questions, len(raw))
	for id, item := range raw {
		var question jev.RawQuestion
		if err := json.Unmarshal(item, &question); err != nil {
			return nil, usagef("question %q is not a JSON object: %v", id, err)
		}
		questions[id] = question
	}
	return questions, nil
}

// runNoul asks one yes/no question.
func runNoul(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("noul", stderr)
	var options clientOptions
	options.register(fs)
	var state stateFlags
	state.register(fs)
	instructions := fs.String("instructions", "", "the yes/no question to evaluate (required)")
	trueDescription := fs.String("true", "", "description of what a yes answer means")
	falseDescription := fs.String("false", "", "description of what a no answer means")
	if err := parse(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*instructions) == "" {
		return usagef("--instructions is required")
	}
	question := jev.Noul{Instructions: *instructions}
	if *trueDescription != "" || *falseDescription != "" {
		criteria := &jev.NoulCriteria{}
		if *trueDescription != "" {
			criteria.True = *trueDescription
		}
		if *falseDescription != "" {
			criteria.False = *falseDescription
		}
		question.Criteria = criteria
	}
	return runQuestion(stdin, stdout, &options, &state, "answer", question)
}

// runChoice asks one choice question.
func runChoice(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("choice", stderr)
	var options clientOptions
	options.register(fs)
	var state stateFlags
	state.register(fs)
	instructions := fs.String("instructions", "", "what the model should decide (required)")
	var optionFlags stringList
	fs.Var(&optionFlags, "option", "choice option as value or value=description; repeat for every option (required)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*instructions) == "" {
		return usagef("--instructions is required")
	}
	if len(optionFlags) == 0 {
		return usagef("at least one --option is required")
	}
	criteria := make(jev.Choices, len(optionFlags))
	for _, option := range optionFlags {
		value, description := splitDescription(option)
		if value == "" {
			return usagef("--option value must not be empty")
		}
		if description == "" {
			criteria[value] = nil
		} else {
			criteria[value] = description
		}
	}
	question := jev.Choice{Instructions: *instructions, Criteria: criteria}
	return runQuestion(stdin, stdout, &options, &state, "answer", question)
}

// runScore asks one score question.
func runScore(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("score", stderr)
	var options clientOptions
	options.register(fs)
	var state stateFlags
	state.register(fs)
	instructions := fs.String("instructions", "", "what the model should rate (required)")
	var levels stringList
	fs.Var(&levels, "level", "score level description, lowest first; repeat for every level (required)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*instructions) == "" {
		return usagef("--instructions is required")
	}
	if len(levels) == 0 {
		return usagef("at least one --level is required")
	}
	criteria := make([]any, len(levels))
	for index, level := range levels {
		criteria[index] = level
	}
	question := jev.Score{Instructions: *instructions, Criteria: criteria}
	return runQuestion(stdin, stdout, &options, &state, "answer", question)
}

// runQuestion evaluates one question and writes the answer.
func runQuestion(stdin io.Reader, stdout io.Writer, options *clientOptions, state *stateFlags, id string, question jev.Question) error {
	stateValue, err := state.read(stdin)
	if err != nil {
		return err
	}
	client, err := options.client()
	if err != nil {
		return err
	}
	response, err := client.SystemOne(context.Background(), jev.SystemOneRequest{
		State:     stateValue,
		Questions: jev.Questions{id: question},
	})
	if err != nil {
		return err
	}
	return writeResponse(stdout, response, options.jsonOut)
}

// batchRequest is one entry of a batch file.
type batchRequest struct {
	State     json.RawMessage            `json:"state"`
	Model     string                     `json:"model"`
	Questions map[string]jev.RawQuestion `json:"questions"`
}

// runBatch streams a JSON array of requests and prints one NDJSON result line
// per request.
func runBatch(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("batch", stderr)
	var options clientOptions
	options.registerConnection(fs)
	var path string
	fs.StringVar(&path, "file", "", "path to a JSON array of requests, or - for stdin (required)")
	concurrency := fs.Int("concurrency", 4, "number of requests in flight")
	ordered := fs.Bool("ordered", false, "emit results in input order")
	if err := parse(fs, args); err != nil {
		return err
	}
	if path == "" {
		return usagef("--file is required")
	}
	requests, err := readBatchRequests(path, stdin)
	if err != nil {
		return err
	}
	client, err := options.client()
	if err != nil {
		return err
	}
	results, err := client.StreamSystemOne(context.Background(), requests,
		jev.WithStreamConcurrency(*concurrency),
		jev.WithStreamOrdered(*ordered),
	)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	failures := 0
	for result := range results {
		entry := map[string]any{"index": result.Index}
		if result.Err != nil {
			entry["error"] = result.Err.Error()
			failures++
		} else {
			entry["response"] = result.Response
		}
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d requests failed", failures, len(requests))
	}
	return nil
}

// readBatchRequests reads a JSON array of requests.
func readBatchRequests(path string, stdin io.Reader) ([]jev.SystemOneRequest, error) {
	data, err := readResource(path, stdin)
	if err != nil {
		return nil, err
	}
	var raw []batchRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, usagef("batch file must be a JSON array of requests: %v", err)
	}
	if len(raw) == 0 {
		return nil, usagef("batch file contains no requests")
	}
	requests := make([]jev.SystemOneRequest, len(raw))
	for index, item := range raw {
		questions := make(jev.Questions, len(item.Questions))
		for id, question := range item.Questions {
			questions[id] = question
		}
		requests[index] = jev.SystemOneRequest{
			State:     item.State,
			Model:     item.Model,
			Questions: questions,
		}
	}
	return requests, nil
}

// writeResponse writes a response as JSON or as a compact table.
func writeResponse(w io.Writer, response *jev.SystemOneResponse, asJSON bool) error {
	if asJSON {
		return writeJSON(w, response)
	}
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	ids := make([]string, 0, len(response.Answers))
	for id := range response.Answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		switch answer := response.Answers[id].(type) {
		case jev.NoulAnswer:
			_, _ = fmt.Fprintf(table, "%s\tnoul\t%s\t\t\n", id, formatFloat(answer.Noul))
		case jev.ChoiceAnswer:
			_, _ = fmt.Fprintf(table, "%s\tchoice\t%s\tconfidence %s\t%s\n",
				id, answer.Choice, formatFloat(answer.Confidence), formatStringProbabilities(answer.Probabilities))
		case jev.ScoreAnswer:
			_, _ = fmt.Fprintf(table, "%s\tscore\t%s\tconfidence %s\t%s\n",
				id, formatFloat(answer.Score), formatFloat(answer.Confidence), formatIntProbabilities(answer.Probabilities))
		case jev.UnknownAnswer:
			_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t\t\n", id, answer.Type, string(answer.Raw))
		}
	}
	return table.Flush()
}

// writeJSON writes indented JSON.
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// formatFloat renders a number for the table output.
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', 4, 64)
}

// formatStringProbabilities renders a choice distribution with sorted keys.
func formatStringProbabilities(probabilities map[string]float64) string {
	keys := make([]string, 0, len(probabilities))
	for key := range probabilities {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+" "+formatFloat(probabilities[key]))
	}
	return strings.Join(parts, "  ")
}

// formatIntProbabilities renders a score distribution with sorted levels.
func formatIntProbabilities(probabilities map[int]float64) string {
	keys := make([]int, 0, len(probabilities))
	for key := range probabilities {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strconv.Itoa(key)+" "+formatFloat(probabilities[key]))
	}
	return strings.Join(parts, "  ")
}

// stringList collects a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

// splitDescription splits "value=description" at the first equals sign.
func splitDescription(raw string) (value, description string) {
	value, description, _ = strings.Cut(raw, "=")
	return strings.TrimSpace(value), strings.TrimSpace(description)
}
