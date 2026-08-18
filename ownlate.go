// Package ownlate is the Go client for OwnLate translations. It mirrors
// @globalart/ownlate-nestjs-translator: translations are pulled from the OTA
// endpoint or from the translations map of a project, kept in memory, refreshed
// in the background and looked up by namespace, key and locale.
package ownlate

import (
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// DefaultBaseURL is the public API the translations are loaded from.
const DefaultBaseURL = "https://api.ownlate.com/public/v1"

// OTANamespace is the namespace an OTA bundle lands in when it carries no
// prefix of its own.
const OTANamespace = "__ota__"

const (
	// DefaultPollInterval is how often the translations are refreshed.
	DefaultPollInterval = 5 * time.Minute
	// DefaultRetryInterval is how long the client waits before retrying a
	// failed load.
	DefaultRetryInterval = 5 * time.Second
	// DefaultTimeout bounds a single request to the API.
	DefaultTimeout = 30 * time.Second
)

// Source describes where the translations come from.
type Source interface {
	validate() error
}

// OTASource loads published bundles by their access keys.
type OTASource struct {
	Bundles []OTABundle
}

// OTABundle is one published bundle. Prefix is the namespace its keys are
// stored under; empty means [OTANamespace].
type OTABundle struct {
	AccessKey string
	Prefix    string
}

func (s OTASource) validate() error {
	if len(s.Bundles) == 0 {
		return errors.New("ownlate: OTA source needs at least one bundle")
	}
	for _, bundle := range s.Bundles {
		if bundle.AccessKey == "" {
			return errors.New("ownlate: OTA bundle needs an access key")
		}
	}

	return nil
}

// MapSource loads the translations map of a project. FilesMap renames a file
// into a namespace; files that are not listed keep their name without the
// .json suffix.
type MapSource struct {
	ProjectID string
	APIKey    string
	FilesMap  map[string]string
}

func (s MapSource) validate() error {
	if s.ProjectID == "" {
		return errors.New("ownlate: map source needs a project id")
	}

	return nil
}

// Config configures a [Client].
type Config struct {
	// Source is where the translations come from. Required.
	Source Source
	// Locale is used by Translate when the call site does not pass one.
	Locale string
	// BaseURL overrides the API address; defaults to [DefaultBaseURL].
	BaseURL string
	// PollInterval is how often Start refreshes the translations;
	// defaults to [DefaultPollInterval].
	PollInterval time.Duration
	// RetryInterval is how long Start waits after a failed load;
	// defaults to [DefaultRetryInterval].
	RetryInterval time.Duration
	// HTTPClient performs the requests; defaults to a client with
	// [DefaultTimeout].
	HTTPClient *http.Client
	// Logger receives the load failures; defaults to slog.Default().
	Logger *slog.Logger
}

func (c *Config) applyDefaults() {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.RetryInterval <= 0 {
		c.RetryInterval = DefaultRetryInterval
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: DefaultTimeout}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

func (c Config) validate() error {
	if c.Source == nil {
		return errors.New("ownlate: source is required")
	}

	return c.Source.validate()
}
