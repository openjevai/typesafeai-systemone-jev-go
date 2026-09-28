package jev

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// SDK defaults, used when no option or environment variable overrides them.
const (
	// DefaultBaseURL is the TypeSafe API root.
	DefaultBaseURL = "https://api.typesafe.ai"
	// DefaultModel handles requests that do not name a model.
	DefaultModel = ModelJevLatest
	// DefaultTimeout is the per-attempt timeout applied when neither
	// WithTimeout nor WithCallTimeout sets one.
	DefaultTimeout = 10 * time.Second
)

// OpenJEV is a community gateway to the same Jev model. These constants let
// callers point the client at it alongside TypeSafe. See https://openjev.sh.
const (
	// OpenJEVBaseURL is the OpenJEV API root.
	OpenJEVBaseURL = "https://api.openjev.sh"
	// ModelOpenJEV is the model id accepted by the OpenJEV gateway.
	ModelOpenJEV = "openjev"
)

// Model identifiers accepted by the model field. Versioned IDs stay put even
// as aliases move; pin one when thresholds are tuned against a release.
const (
	// ModelJevLatest is the most recent stable, official Jev release.
	ModelJevLatest = "jev-latest"
	// ModelJevPreview is the most recent release, official or not.
	ModelJevPreview = "jev-preview"
	// ModelJev1130 is the pinned Jev 1.13.0 release.
	ModelJev1130 = "jev-1.13.0"
)

// Environment variables read by NewClient. Explicit options take precedence.
const (
	// EnvAPIKey holds the API key. Required unless WithAPIKey is used.
	EnvAPIKey = "TYPESAFE_API_KEY"
	// EnvBaseURL overrides DefaultBaseURL.
	EnvBaseURL = "TYPESAFE_BASE_URL"
	// EnvModel overrides DefaultModel.
	EnvModel = "TYPESAFE_DEFAULT_MODEL"
	// EnvLogLevel enables SDK logging on stderr at the given level; one of
	// "debug", "info", "warning" (or "warn"), "error", or "off".
	EnvLogLevel = "TYPESAFE_LOG_LEVEL"
)

// OpenJEV environment variables. OpenJEV is a community gateway to the same
// Jev model; TypeSafe remains the default. Set JEV_PROVIDER=openjev or rely
// on key availability (see NewClient).
const (
	// EnvOpenJEVAPIKey holds the OpenJEV API key. Used when JEV_PROVIDER=openjev
	// or when only OPENJEV_API_KEY is set and TYPESAFE_API_KEY is not.
	EnvOpenJEVAPIKey = "OPENJEV_API_KEY"
	// EnvProvider selects the API provider: "typesafe" (default) or "openjev".
	// When unset, the provider is chosen by key availability: TypeSafe if
	// TYPESAFE_API_KEY is set, otherwise OpenJEV if OPENJEV_API_KEY is set.
	EnvProvider = "JEV_PROVIDER"
)

// Client talks to the TypeSafe System One API. It is safe for concurrent use
// by multiple goroutines. Create one with NewClient and reuse it.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	timeout    time.Duration
	retry      RetryPolicy
	header     http.Header
	logger     *slog.Logger
}

// config collects client settings while options and environment variables are
// resolved.
type config struct {
	apiKey      string
	baseURL     string
	model       string
	httpClient  *http.Client
	timeout     time.Duration
	retry       RetryPolicy
	header      http.Header
	logger      *slog.Logger
	logLevel    slog.Level
	logLevelSet bool
}

// Option configures a Client. Options are applied in order after environment
// variables, so they always win.
type Option func(*config) error

// NewClient builds a client. Configuration resolves in this order: the
// environment variables TYPESAFE_API_KEY, TYPESAFE_BASE_URL,
// TYPESAFE_DEFAULT_MODEL, and TYPESAFE_LOG_LEVEL, then the supplied options,
// then the defaults DefaultBaseURL, DefaultModel, DefaultTimeout, and
// DefaultRetryPolicy.
//
// Provider selection: set JEV_PROVIDER=openjev to use the OpenJEV gateway
// (https://openjev.sh), or set only OPENJEV_API_KEY (without
// TYPESAFE_API_KEY) to auto-select it. TypeSafe is the default when
// TYPESAFE_API_KEY is set. The OpenJEV gateway uses model "openjev" and
// endpoint https://api.openjev.sh.
//
// It returns an error wrapping ErrNoAPIKey when no API key is available.
func NewClient(opts ...Option) (*Client, error) {
	cfg := config{
		baseURL: DefaultBaseURL,
		model:   DefaultModel,
		timeout: DefaultTimeout,
		retry:   DefaultRetryPolicy(),
		header:  make(http.Header),
	}
	// Provider selection: JEV_PROVIDER explicitly selects the API provider.
	// When unset, TypeSafe is the default if TYPESAFE_API_KEY is set;
	// otherwise OpenJEV is used if only OPENJEV_API_KEY is set. TypeSafe
	// stays the unchanged default for anyone with a TypeSafe key.
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(EnvProvider)))
	useOpenJEV := provider == "openjev"
	if provider == "" {
		_, hasTypeSafeKey := envValue(EnvAPIKey)
		_, hasOpenJEVKey := envValue(EnvOpenJEVAPIKey)
		if !hasTypeSafeKey && hasOpenJEVKey {
			useOpenJEV = true
		}
	}
	if useOpenJEV {
		cfg.baseURL = OpenJEVBaseURL
		cfg.model = ModelOpenJEV
	}
	if value, ok := envValue(EnvBaseURL); ok {
		cfg.baseURL = value
	}
	if value, ok := envValue(EnvModel); ok {
		cfg.model = value
	}
	if useOpenJEV {
		if value, ok := envValue(EnvOpenJEVAPIKey); ok {
			cfg.apiKey = value
		}
	} else {
		if value, ok := envValue(EnvAPIKey); ok {
			cfg.apiKey = value
		}
	}
	if level, ok := parseLogLevel(os.Getenv(EnvLogLevel)); ok {
		cfg.logLevel = level
		cfg.logLevelSet = true
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.apiKey == "" {
		keyEnv := EnvAPIKey
		if useOpenJEV {
			keyEnv = EnvOpenJEVAPIKey
		}
		return nil, fmt.Errorf("%w: set the %s environment variable or use jev.WithAPIKey", ErrNoAPIKey, keyEnv)
	}
	parsed, err := url.Parse(cfg.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: base URL %q must be an absolute http(s) URL", ErrInvalidConfiguration, cfg.baseURL)
	}
	if cfg.model == "" {
		return nil, fmt.Errorf("%w: default model must not be empty", ErrInvalidConfiguration)
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{}
	}
	if cfg.logger == nil {
		if cfg.logLevelSet {
			cfg.logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.logLevel}))
		} else {
			cfg.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		}
	}
	return &Client{
		apiKey:     cfg.apiKey,
		baseURL:    strings.TrimRight(cfg.baseURL, "/"),
		model:      cfg.model,
		httpClient: cfg.httpClient,
		timeout:    cfg.timeout,
		retry:      cfg.retry,
		header:     cfg.header,
		logger:     cfg.logger,
	}, nil
}

// Model returns the default model used for requests that do not name one.
func (c *Client) Model() string { return c.model }

// BaseURL returns the configured API root.
func (c *Client) BaseURL() string { return c.baseURL }

// WithAPIKey sets the API key, taking precedence over TYPESAFE_API_KEY.
func WithAPIKey(apiKey string) Option {
	return func(cfg *config) error {
		cfg.apiKey = strings.TrimSpace(apiKey)
		return nil
	}
}

// WithBaseURL sets the API root, taking precedence over TYPESAFE_BASE_URL.
func WithBaseURL(baseURL string) Option {
	return func(cfg *config) error {
		cfg.baseURL = strings.TrimSpace(baseURL)
		return nil
	}
}

// WithModel sets the model used by requests that do not name one, taking
// precedence over TYPESAFE_DEFAULT_MODEL. Use ModelJevLatest, ModelJevPreview,
// or a pinned ID such as ModelJev1130.
func WithModel(model string) Option {
	return func(cfg *config) error {
		cfg.model = strings.TrimSpace(model)
		return nil
	}
}

// WithHTTPClient supplies the HTTP client used for requests, for example to
// install a custom transport or proxy. The client's own Timeout, if any, also
// applies alongside the per-attempt timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(cfg *config) error {
		if client == nil {
			return fmt.Errorf("%w: HTTP client must not be nil", ErrInvalidConfiguration)
		}
		cfg.httpClient = client
		return nil
	}
}

// WithTimeout sets the per-attempt HTTP timeout, taking precedence over
// DefaultTimeout. Use zero to disable the per-attempt timeout and rely on
// context deadlines and the retry budget instead.
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *config) error {
		if timeout < 0 {
			return fmt.Errorf("%w: timeout must not be negative", ErrInvalidConfiguration)
		}
		cfg.timeout = timeout
		return nil
	}
}

// WithRetryPolicy sets the retry behavior, replacing DefaultRetryPolicy. Pass
// RetryPolicy{} to disable retries.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(cfg *config) error {
		if err := policy.validate(); err != nil {
			return err
		}
		cfg.retry = policy
		return nil
	}
}

// WithHeader adds a header sent with every request. SDK headers such as
// Authorization and User-Agent cannot be overridden.
func WithHeader(key, value string) Option {
	return func(cfg *config) error {
		cfg.header.Set(key, value)
		return nil
	}
}

// WithHeaders adds headers sent with every request.
func WithHeaders(headers http.Header) Option {
	return func(cfg *config) error {
		for key, values := range headers {
			for _, value := range values {
				cfg.header.Add(key, value)
			}
		}
		return nil
	}
}

// WithLogger sets the logger used for debug and info diagnostics. The SDK
// never logs request or response bodies; the API key is never logged.
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *config) error {
		if logger == nil {
			return fmt.Errorf("%w: logger must not be nil", ErrInvalidConfiguration)
		}
		cfg.logger = logger
		return nil
	}
}

// WithLogLevel logs SDK diagnostics to stderr at the given level. It is
// overridden by WithLogger and supersedes TYPESAFE_LOG_LEVEL.
func WithLogLevel(level slog.Level) Option {
	return func(cfg *config) error {
		cfg.logLevel = level
		cfg.logLevelSet = true
		return nil
	}
}

// CallOption configures a single API call.
type CallOption func(*callConfig) error

// callConfig collects per-call overrides.
type callConfig struct {
	timeout    time.Duration
	timeoutSet bool
	retry      RetryPolicy
	retrySet   bool
	header     http.Header
}

// WithCallTimeout sets the per-attempt HTTP timeout for one call, overriding
// the client setting.
func WithCallTimeout(timeout time.Duration) CallOption {
	return func(cc *callConfig) error {
		if timeout < 0 {
			return fmt.Errorf("%w: call timeout must not be negative", ErrInvalidConfiguration)
		}
		cc.timeout = timeout
		cc.timeoutSet = true
		return nil
	}
}

// WithCallRetryPolicy sets the retry policy for one call, overriding the
// client policy.
func WithCallRetryPolicy(policy RetryPolicy) CallOption {
	return func(cc *callConfig) error {
		if err := policy.validate(); err != nil {
			return err
		}
		cc.retry = policy
		cc.retrySet = true
		return nil
	}
}

// WithCallHeader adds a header sent with one call.
func WithCallHeader(key, value string) CallOption {
	return func(cc *callConfig) error {
		cc.header.Set(key, value)
		return nil
	}
}

// WithCallHeaders adds headers sent with one call.
func WithCallHeaders(headers http.Header) CallOption {
	return func(cc *callConfig) error {
		for key, values := range headers {
			for _, value := range values {
				cc.header.Add(key, value)
			}
		}
		return nil
	}
}

// resolveCallOptions applies call options in order.
func resolveCallOptions(opts []CallOption) (callConfig, error) {
	cc := callConfig{header: make(http.Header)}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cc); err != nil {
			return cc, err
		}
	}
	return cc, nil
}

// envValue returns a trimmed, non-empty environment variable.
func envValue(name string) (string, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	return value, value != ""
}

// parseLogLevel maps TYPESAFE_LOG_LEVEL values to slog levels. The value
// "off" maps to a level above error so nothing is logged.
func parseLogLevel(raw string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	case "off":
		return slog.Level(100), true
	default:
		return 0, false
	}
}
