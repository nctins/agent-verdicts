// Package providerhttp is the shared, key-safe HTTP plumbing for the
// embedding and rerank provider clients.
package providerhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxBody    = 32 << 20
	maxErrBody = 300
)

// NewClient returns hc (or a fresh client when nil) with a redirect policy that
// drops Authorization unless scheme and host:port are unchanged (Go itself only
// compares hostnames, so a same-host other-port redirect would keep the key).
// A caller-supplied CheckRedirect is left untouched: the caller opts out.
func NewClient(hc *http.Client) *http.Client {
	if hc == nil {
		hc = &http.Client{}
	}
	if hc.CheckRedirect != nil {
		return hc
	}
	cp := *hc
	cp.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if r.URL.Scheme != via[0].URL.Scheme || r.URL.Host != via[0].URL.Host {
			r.Header.Del("Authorization")
		}
		return nil
	}
	return &cp
}

// PostJSON POSTs body as JSON, with a Bearer header when apiKey != "". It returns
// the response body (capped at 32 MB; exceeding it is an error). Non-2xx yields an
// error with the status and up to 300 bytes of body, with apiKey redacted.
func PostJSON(ctx context.Context, hc *http.Client, url, apiKey string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", redact(err, apiKey))
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", redact(err, apiKey))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", redact(err, apiKey))
	}
	if len(data) > maxBody {
		return nil, fmt.Errorf("response too large (> %d bytes)", maxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if len(data) > maxErrBody {
			data = data[:maxErrBody]
		}
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, redactStr(string(data), apiKey))
	}
	return data, nil
}

func redactStr(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "***")
}

func redact(err error, key string) error {
	if key == "" {
		return err
	}
	return errors.New(redactStr(err.Error(), key))
}
