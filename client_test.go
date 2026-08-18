package ownlate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func otaServer(t *testing.T, bundles map[string]map[string]map[string]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accessKey := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ota/"), "/bundles")
		translations, ok := bundles[accessKey]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":      1,
			"languages":    []string{"ru", "en_US"},
			"translations": translations,
		})
	}))
}

func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := client.Load(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client
}

func TestTranslateOTABundle(t *testing.T) {
	server := otaServer(t, map[string]map[string]map[string]string{
		"key-1": {
			"ru":    {"email": "Почта", "greeting": "Привет, {{name}}"},
			"en_US": {"email": "Email"},
		},
	})
	defer server.Close()

	client := newTestClient(t, Config{
		Source:  OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		Locale:  "ru",
		BaseURL: server.URL,
	})

	if got := client.Translate(OTANamespace, "email", nil, ""); got != "Почта" {
		t.Fatalf("default locale: got %q", got)
	}
	if got := client.Translate(OTANamespace, "email", nil, "en_US"); got != "Email" {
		t.Fatalf("explicit locale: got %q", got)
	}
	if got := client.T("email", "en_US"); got != "Email" {
		t.Fatalf("T: got %q", got)
	}
	if got := client.Translate(OTANamespace, "missing", nil, "ru"); got != "missing" {
		t.Fatalf("unknown key must fall back to itself: got %q", got)
	}
	if got := client.Translate("other", "email", nil, "ru"); got != "Почта" {
		t.Fatalf("unknown namespace must fall back to the OTA one: got %q", got)
	}
}

func TestTranslatePlaceholders(t *testing.T) {
	server := otaServer(t, map[string]map[string]map[string]string{
		"key-1": {"ru": {"greeting": "Привет, {{name}}! Осталось {{count}} дней, {{name}}."}},
	})
	defer server.Close()

	client := newTestClient(t, Config{
		Source:  OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		Locale:  "ru",
		BaseURL: server.URL,
	})

	got := client.Translate(OTANamespace, "greeting", map[string]any{"name": "Роман", "count": 3}, "")
	if got != "Привет, Роман! Осталось 3 дней, Роман." {
		t.Fatalf("unexpected text: %q", got)
	}
}

func TestUnknownLocaleFallsBackToTheFirstOne(t *testing.T) {
	server := otaServer(t, map[string]map[string]map[string]string{
		"key-1": {
			"ru":    {"email": "Почта"},
			"en_US": {"email": "Email"},
		},
	})
	defer server.Close()

	client := newTestClient(t, Config{
		Source:  OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		BaseURL: server.URL,
	})

	// Locales are sorted, so the fallback is deterministic.
	if got := client.Translate(OTANamespace, "email", nil, "de"); got != "Email" {
		t.Fatalf("unexpected fallback: %q", got)
	}
	locales := client.Locales(OTANamespace)
	if len(locales) != 2 || locales[0] != "en_US" || locales[1] != "ru" {
		t.Fatalf("unexpected locales: %v", locales)
	}
}

func TestPrefixedBundlesAreSeparateNamespaces(t *testing.T) {
	server := otaServer(t, map[string]map[string]map[string]string{
		"key-1": {"ru": {"title": "Письмо"}},
		"key-2": {"ru": {"title": "Пуш"}},
	})
	defer server.Close()

	client := newTestClient(t, Config{
		Source: OTASource{Bundles: []OTABundle{
			{AccessKey: "key-1", Prefix: "email"},
			{AccessKey: "key-2", Prefix: "push"},
		}},
		Locale:  "ru",
		BaseURL: server.URL,
	})

	if got := client.Translate("email", "title", nil, ""); got != "Письмо" {
		t.Fatalf("unexpected email title: %q", got)
	}
	if got := client.Translate("push", "title", nil, ""); got != "Пуш" {
		t.Fatalf("unexpected push title: %q", got)
	}
}

func TestBundlesSharingANamespaceAreMerged(t *testing.T) {
	server := otaServer(t, map[string]map[string]map[string]string{
		"key-1": {"ru": {"a": "1"}},
		"key-2": {"ru": {"b": "2"}, "en_US": {"a": "one"}},
	})
	defer server.Close()

	client := newTestClient(t, Config{
		Source: OTASource{Bundles: []OTABundle{
			{AccessKey: "key-1", Prefix: "shared"},
			{AccessKey: "key-2", Prefix: "shared"},
		}},
		Locale:  "ru",
		BaseURL: server.URL,
	})

	if got := client.Translate("shared", "a", nil, "ru"); got != "1" {
		t.Fatalf("unexpected a: %q", got)
	}
	if got := client.Translate("shared", "b", nil, "ru"); got != "2" {
		t.Fatalf("unexpected b: %q", got)
	}
	if got := client.Translate("shared", "a", nil, "en_US"); got != "one" {
		t.Fatalf("unexpected en_US a: %q", got)
	}
}

func TestLoadMapSource(t *testing.T) {
	var gotQuery, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("projectId")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"common.json": map[string]any{"ru": map[string]string{"yes": "Да"}},
			"emails.json": map[string]any{"ru": map[string]string{"subject": "Тема"}},
		})
	}))
	defer server.Close()

	client := newTestClient(t, Config{
		Source: MapSource{
			ProjectID: "42",
			APIKey:    "secret",
			FilesMap:  map[string]string{"emails.json": "email"},
		},
		Locale:  "ru",
		BaseURL: server.URL,
	})

	if gotQuery != "42" {
		t.Fatalf("unexpected projectId: %q", gotQuery)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("unexpected authorization: %q", gotAuth)
	}
	if got := client.Translate("common", "yes", nil, ""); got != "Да" {
		t.Fatalf("file name became the namespace: got %q", got)
	}
	if got := client.Translate("email", "subject", nil, ""); got != "Тема" {
		t.Fatalf("filesMap rename failed: got %q", got)
	}
	// A map source has no OTA namespace to fall back to.
	if got := client.Translate("unknown", "yes", nil, ""); got != "yes" {
		t.Fatalf("unexpected fallback: %q", got)
	}
}

func TestLoadRejectsAnInvalidPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"translations": "nope"}`))
	}))
	defer server.Close()

	client, err := New(Config{
		Source:  OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Load(context.Background()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestLoadReportsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := New(Config{
		Source:  OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := client.Load(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestTranslateBeforeTheFirstLoad(t *testing.T) {
	client, err := New(Config{Source: OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := client.Translate(OTANamespace, "email", nil, "ru"); got != "email" {
		t.Fatalf("an empty store must echo the key: %q", got)
	}
}

func TestStartRefreshesAndRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"translations": map[string]map[string]string{"ru": {"email": "Почта"}},
		})
	}))
	defer server.Close()

	client, err := New(Config{
		Source:        OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		Locale:        "ru",
		BaseURL:       server.URL,
		PollInterval:  time.Hour,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	client.Start(context.Background())

	select {
	case <-client.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("the client never became ready")
	}

	if got := client.Translate(OTANamespace, "email", nil, ""); got != "Почта" {
		t.Fatalf("unexpected translation after retry: %q", got)
	}
	if calls.Load() < 2 {
		t.Fatalf("expected a retry, got %d calls", calls.Load())
	}
}

func TestStartStopsWithTheContext(t *testing.T) {
	server := otaServer(t, map[string]map[string]map[string]string{
		"key-1": {"ru": {"email": "Почта"}},
	})
	defer server.Close()

	client, err := New(Config{
		Source:       OTASource{Bundles: []OTABundle{{AccessKey: "key-1"}}},
		BaseURL:      server.URL,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	client.Start(ctx)

	select {
	case <-client.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("the client never became ready")
	}

	cancel()

	select {
	case <-client.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the polling loop did not stop")
	}
}

func TestNewValidatesTheConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a client without a source must be rejected")
	}
	if _, err := New(Config{Source: OTASource{}}); err == nil {
		t.Fatal("an OTA source without bundles must be rejected")
	}
	if _, err := New(Config{Source: OTASource{Bundles: []OTABundle{{}}}}); err == nil {
		t.Fatal("a bundle without an access key must be rejected")
	}
	if _, err := New(Config{Source: MapSource{}}); err == nil {
		t.Fatal("a map source without a project id must be rejected")
	}
}
