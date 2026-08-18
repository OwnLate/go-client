package ownlate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrInvalidResponse is returned when the API answers with a body that is not
// a translations payload.
var ErrInvalidResponse = errors.New("ownlate: failed to load translations: invalid response format")

// translations is namespace -> locale -> key -> text.
type translations map[string]map[string]map[string]string

// Client keeps the translations in memory and refreshes them in the
// background. It is safe for concurrent use.
type Client struct {
	cfg Config

	mu    sync.RWMutex
	store translations

	loading sync.Mutex

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	stopped   chan struct{}

	readyOnce sync.Once
	ready     chan struct{}
}

// New builds a client. Nothing is loaded yet: call [Client.Load] once or
// [Client.Start] to keep the translations fresh.
func New(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	return &Client{
		cfg:     cfg,
		store:   translations{},
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
		ready:   make(chan struct{}),
	}, nil
}

// NewOTA is the shorthand for a client backed by a single published bundle.
func NewOTA(accessKey string, locale string) (*Client, error) {
	return New(Config{
		Source: OTASource{Bundles: []OTABundle{{AccessKey: accessKey}}},
		Locale: locale,
	})
}

// Load fetches the translations once and replaces the in-memory copy.
func (c *Client) Load(ctx context.Context) error {
	c.loading.Lock()
	defer c.loading.Unlock()

	var (
		loaded translations
		err    error
	)
	switch source := c.cfg.Source.(type) {
	case OTASource:
		loaded, err = c.loadOTA(ctx, source)
	case MapSource:
		loaded, err = c.loadMap(ctx, source)
	default:
		err = fmt.Errorf("ownlate: unsupported source %T", c.cfg.Source)
	}
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.store = loaded
	c.mu.Unlock()

	c.readyOnce.Do(func() { close(c.ready) })

	return nil
}

// Start loads the translations and keeps refreshing them until the context is
// cancelled or [Client.Close] is called. A failed load is logged and retried
// after Config.RetryInterval, so start-up is never blocked by the API being
// down. Calling Start more than once is a no-op.
func (c *Client) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		go c.loop(ctx)
	})
}

// Ready is closed after the first successful load, so callers that need
// translations before serving traffic can wait for it.
func (c *Client) Ready() <-chan struct{} { return c.ready }

// Close stops the background refresh.
func (c *Client) Close() error {
	c.stopOnce.Do(func() {
		close(c.stop)
	})

	return nil
}

func (c *Client) loop(ctx context.Context) {
	defer close(c.stopped)

	timer := newTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		case <-timer.C:
			delay := c.cfg.PollInterval
			if err := c.Load(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				c.cfg.Logger.Error("failed to load translations", "error", err)
				delay = c.cfg.RetryInterval
			}
			timer.Reset(delay)
		}
	}
}

// Translate resolves a key. Unknown keys, namespaces and locales fall back to
// the key itself, so a missing translation never blanks out the response.
// Placeholders replace every {{name}} occurrence in the text.
func (c *Client) Translate(namespace, key string, placeholders map[string]any, locale string) string {
	if locale == "" {
		locale = c.cfg.Locale
	}

	text := c.lookup(namespace, key, locale)
	if len(placeholders) == 0 {
		return text
	}

	for name, value := range placeholders {
		text = strings.ReplaceAll(text, "{{"+name+"}}", fmt.Sprint(value))
	}

	return text
}

// T translates a key in the OTA namespace, the common case for a service that
// publishes one bundle.
func (c *Client) T(key, locale string) string {
	return c.Translate(OTANamespace, key, nil, locale)
}

// Locales returns the locales a namespace carries, sorted.
func (c *Client) Locales(namespace string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	byLocale, ok := c.store[namespace]
	if !ok {
		return nil
	}

	locales := make([]string, 0, len(byLocale))
	for locale := range byLocale {
		locales = append(locales, locale)
	}
	sort.Strings(locales)

	return locales
}

func (c *Client) lookup(namespace, key, locale string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	byLocale, ok := c.store[namespace]
	if !ok {
		// An OTA bundle without a prefix lands in the default namespace, and
		// callers should not have to know that.
		if _, isOTA := c.cfg.Source.(OTASource); isOTA {
			byLocale, ok = c.store[OTANamespace]
		}
		if !ok {
			return key
		}
	}

	byKey := resolveLocale(byLocale, locale)
	if text, ok := byKey[key]; ok {
		return text
	}

	return key
}

// resolveLocale looks for an exact match first, then for a locale sharing the
// same language (so that en_US reaches en and the other way round), and only
// then falls back to the first locale of the namespace, ordered by name so that
// the choice stays stable between calls.
func resolveLocale(byLocale map[string]map[string]string, locale string) map[string]string {
	locales := make([]string, 0, len(byLocale))
	for name := range byLocale {
		locales = append(locales, name)
	}
	if len(locales) == 0 {
		return nil
	}
	sort.Strings(locales)

	if locale != "" {
		if byKey, ok := byLocale[locale]; ok {
			return byKey
		}

		language := languageOf(locale)
		for _, name := range locales {
			if languageOf(name) == language {
				return byLocale[name]
			}
		}
	}

	return byLocale[locales[0]]
}

// languageOf strips the region, turning en_US and en-US into en.
func languageOf(locale string) string {
	if index := strings.IndexAny(locale, "_-"); index > 0 {
		return strings.ToLower(locale[:index])
	}

	return strings.ToLower(locale)
}

func mergeLocales(existing, incoming map[string]map[string]string) map[string]map[string]string {
	if existing == nil {
		return incoming
	}

	for locale, keys := range incoming {
		if _, ok := existing[locale]; !ok {
			existing[locale] = map[string]string{}
		}
		for key, text := range keys {
			existing[locale][key] = text
		}
	}

	return existing
}
