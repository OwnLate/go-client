package ownlate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// otaBundleResponse is the payload of GET /ota/{accessKey}/bundles.
type otaBundleResponse struct {
	Version      *int                         `json:"version"`
	Languages    []string                     `json:"languages"`
	PublishedAt  string                       `json:"publishedAt"`
	Translations map[string]map[string]string `json:"translations"`
}

func (c *Client) loadOTA(ctx context.Context, source OTASource) (translations, error) {
	loaded := translations{}

	for _, bundle := range source.Bundles {
		byLocale, err := c.fetchOTABundle(ctx, bundle.AccessKey)
		if err != nil {
			return nil, err
		}

		namespace := bundle.Prefix
		if namespace == "" {
			namespace = OTANamespace
		}
		loaded[namespace] = mergeLocales(loaded[namespace], byLocale)
	}

	return loaded, nil
}

func (c *Client) fetchOTABundle(ctx context.Context, accessKey string) (map[string]map[string]string, error) {
	endpoint := fmt.Sprintf("%s/ota/%s/bundles", strings.TrimRight(c.cfg.BaseURL, "/"), url.PathEscape(accessKey))

	var payload otaBundleResponse
	if err := c.get(ctx, endpoint, nil, nil, &payload); err != nil {
		return nil, err
	}
	if payload.Translations == nil {
		return nil, ErrInvalidResponse
	}

	return payload.Translations, nil
}

func (c *Client) loadMap(ctx context.Context, source MapSource) (translations, error) {
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/segments/translations-map"

	headers := map[string]string{}
	if source.APIKey != "" {
		headers["Authorization"] = "Bearer " + source.APIKey
	}
	query := url.Values{"projectId": []string{source.ProjectID}}

	// file name -> locale -> key -> text
	var payload map[string]map[string]map[string]string
	if err := c.get(ctx, endpoint, query, headers, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, ErrInvalidResponse
	}

	loaded := translations{}
	for fileName, byLocale := range payload {
		namespace, ok := source.FilesMap[fileName]
		if !ok {
			namespace = strings.TrimSuffix(fileName, ".json")
		}
		loaded[namespace] = byLocale
	}

	return loaded, nil
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, headers map[string]string, target any) error {
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := c.cfg.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("ownlate: request to %s failed: %w", endpoint, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("ownlate: %s returned %d", endpoint, response.StatusCode)
	}

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidResponse, err)
	}

	return nil
}

// timer wraps time.Timer so that the polling loop can start immediately and
// then reset itself to the poll or retry delay.
type timer struct{ *time.Timer }

func newTimer(delay time.Duration) timer {
	if delay <= 0 {
		delay = time.Nanosecond
	}

	return timer{time.NewTimer(delay)}
}

func (t timer) Reset(delay time.Duration) {
	if !t.Timer.Stop() {
		select {
		case <-t.Timer.C:
		default:
		}
	}
	t.Timer.Reset(delay)
}
