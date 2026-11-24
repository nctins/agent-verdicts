package httpapi

import (
	"errors"
	"strings"
	"testing"
)

const secretKey = "sk-very-secret-9876"

// noKey fails if any response body contains the API key (or any key given).
func noKey(t *testing.T, r resp, keys ...string) resp {
	t.Helper()
	for _, k := range append(keys, secretKey) {
		if strings.Contains(r.raw, k) {
			t.Fatalf("response contains an API key: %s", r.raw)
		}
	}
	return r
}

func settingsBody(embURL string, key any) map[string]any {
	emb := map[string]any{"url": embURL, "model": "e1"}
	if key != "absent" {
		emb["api_key"] = key
	}
	return map[string]any{"embedding": emb, "rerank": map[string]any{"url": "", "model": ""}}
}

func TestSettings_GetPutKeySemantics(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := noKey(t, c.do("GET", "/admin/settings/search", nil, nil))
	wantStatus(t, r, 200)
	emb, _ := r.body["embedding"].(map[string]any)
	st, _ := r.body["status"].(map[string]any)
	if emb["url"] != "" || emb["api_key_last4"] != "" || emb["dim"] != 0.0 || st["pipeline"] != "bm25" ||
		st["reindexing"] != false || st["embedded_count"] != 0.0 || st["total_count"] != 0.0 {
		t.Fatalf("initial GET = %s", r.raw)
	}
	if w, ok := st["warnings"].([]any); !ok || len(w) != 0 {
		t.Fatalf("warnings must be []: %s", r.raw)
	}
	if _, ok := r.body["rerank"].(map[string]any)["api_key_last4"]; !ok {
		t.Fatalf("rerank lacks api_key_last4: %s", r.raw)
	}

	// Set a key.
	r = noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example/v1", secretKey), nil))
	wantStatus(t, r, 200)
	emb = r.body["embedding"].(map[string]any)
	if emb["url"] != "http://emb.example/v1" || emb["model"] != "e1" || emb["api_key_last4"] != "9876" || emb["dim"] != 3.0 {
		t.Fatalf("PUT = %s", r.raw)
	}
	if _, ok := r.body["status"].(map[string]any); !ok {
		t.Fatalf("PUT response lacks status: %s", r.raw)
	}
	if got := e.settings.Current().Embedding.APIKey; got != secretKey {
		t.Fatalf("stored key = %q", got)
	}
	// Absent keeps it.
	r = noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example/v2", "absent"), nil))
	wantStatus(t, r, 200)
	if r.body["embedding"].(map[string]any)["api_key_last4"] != "9876" || e.settings.Current().Embedding.APIKey != secretKey {
		t.Fatalf("absent key not kept: %s", r.raw)
	}
	// Changing host without re-entering it is refused.
	wantErr(t, noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://other.example", "absent"), nil)), 400, "invalid_argument")
	// Empty or blank keeps it too (spec §6.6); only null deletes.
	for _, blank := range []string{"", "   "} {
		r = noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example/v2", blank), nil))
		wantStatus(t, r, 200)
		if r.body["embedding"].(map[string]any)["api_key_last4"] != "9876" || e.settings.Current().Embedding.APIKey != secretKey {
			t.Fatalf("api_key %q did not keep the key: %s", blank, r.raw)
		}
	}
	// Non-string key.
	wantErr(t, noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example/v2", 42), nil)), 400, "invalid_argument")
	// null deletes it.
	r = noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example/v2", nil), nil))
	wantStatus(t, r, 200)
	if r.body["embedding"].(map[string]any)["api_key_last4"] != "" || e.settings.Current().Embedding.APIKey != "" {
		t.Fatalf("null did not delete the key: %s", r.raw)
	}
	wantErr(t, noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("ftp://x", secretKey), nil)), 400, "invalid_argument")
	wantErr(t, c.do("PUT", "/admin/settings/search", `{"embedding":{"url":"","model":"","extra":1}}`, nil), 400, "invalid_argument")
}

func TestSettings_UpstreamFailure502_NotSaved_KeyRedacted(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	wantStatus(t, noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example", "sk-old-key-1234"), nil), "sk-old-key-1234"), 200)

	e.tester.fail(errors.New("401 from provider: bad key " + secretKey))
	for _, path := range []string{"/admin/settings/search", "/admin/settings/search/test"} {
		method := "PUT"
		if strings.HasSuffix(path, "/test") {
			method = "POST"
		}
		r := noKey(t, c.do(method, path, settingsBody("http://emb.example", secretKey), nil))
		wantErr(t, r, 502, "upstream_failed")
		if !strings.Contains(r.body["message"].(string), "401 from provider") {
			t.Fatalf("provider message missing: %s", r.raw)
		}
	}
	// A kept key is redacted too.
	e.tester.fail(errors.New("rejected sk-old-key-1234"))
	wantErr(t, noKey(t, c.do("PUT", "/admin/settings/search", settingsBody("http://emb.example", "absent"), nil), "sk-old-key-1234"), 502, "upstream_failed")

	if got := e.settings.Current().Embedding.APIKey; got != "sk-old-key-1234" {
		t.Fatalf("failed PUT changed the key to %q", got)
	}
	r := noKey(t, c.do("GET", "/admin/settings/search", nil, nil), "sk-old-key-1234")
	if r.body["embedding"].(map[string]any)["api_key_last4"] != "1234" {
		t.Fatalf("GET after failure = %s", r.raw)
	}
}

func TestSettings_TestDoesNotPersist(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := noKey(t, c.do("POST", "/admin/settings/search/test", settingsBody("http://emb.example", secretKey), nil))
	wantStatus(t, r, 204)
	if e.tester.count() != 1 || e.settings.Current().Embedding.URL != "" {
		t.Fatalf("test persisted or did not test: calls=%d cur=%+v", e.tester.count(), e.settings.Current().Embedding.URL)
	}
	wantErr(t, noKey(t, c.do("POST", "/admin/settings/search/test", settingsBody("nope", secretKey), nil)), 400, "invalid_argument")
}

func TestSettings_Reindex202(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := c.do("POST", "/admin/settings/search/reindex", nil, nil)
	wantStatus(t, r, 202)
	if r.body["reindexing"] != true {
		t.Fatalf("reindex = %s", r.raw)
	}
	r = c.do("GET", "/admin/settings/search", nil, nil)
	if r.body["status"].(map[string]any)["reindexing"] != true {
		t.Fatalf("status after reindex = %s", r.raw)
	}
}
