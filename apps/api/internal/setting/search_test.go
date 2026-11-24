package setting_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"agent-verdicts/internal/setting"
)

type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memStore) Get(_ context.Context, k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	if !ok {
		return nil, setting.ErrNotFound
	}
	return v, nil
}

func (s *memStore) Put(_ context.Context, k string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[k] = v
	return nil
}

// okTester records calls and reports dim 1024 for an enabled embedding.
type okTester struct {
	mu    sync.Mutex
	calls []setting.Search
}

func (o *okTester) test(_ context.Context, s setting.Search) (setting.Search, error) {
	o.mu.Lock()
	o.calls = append(o.calls, s)
	o.mu.Unlock()
	if s.Embedding.URL != "" {
		s.Embedding.Dim = 1024
	}
	return s, nil
}

func ptr(s string) *string { return &s }

func newCipher(t *testing.T) *setting.Cipher {
	t.Helper()
	c, err := setting.LoadKey(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newSvc(t *testing.T, st setting.Store, c *setting.Cipher, test setting.Tester) *setting.Service {
	t.Helper()
	s, err := setting.NewService(context.Background(), st, c, test)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var (
	embOn = setting.ProviderUpdate{URL: "http://embed:80/v1", Model: " bge-m3 ", APIKey: ptr(" sk-embed-ABCD ")}
	rerOn = setting.ProviderUpdate{URL: "https://rerank.example", Model: "bge-reranker", APIKey: ptr("rk-key-9876")}
)

func TestService_EmptyStoreIsZeroConfig(t *testing.T) {
	s := newSvc(t, &memStore{}, newCipher(t), (&okTester{}).test)
	if got := s.Current(); got != (setting.Search{}) {
		t.Fatalf("Current() = %+v, want zero", got)
	}
	if len(s.Warnings()) != 0 {
		t.Fatalf("Warnings() = %v", s.Warnings())
	}
}

func TestService_UpdateSavesAndReloads(t *testing.T) {
	ctx := context.Background()
	st, c, tr := &memStore{}, newCipher(t), &okTester{}
	s := newSvc(t, st, c, tr.test)
	got, err := s.Update(ctx, embOn, rerOn)
	if err != nil {
		t.Fatal(err)
	}
	want := setting.Search{
		Embedding: setting.Provider{URL: "http://embed:80/v1", Model: "bge-m3", APIKey: "sk-embed-ABCD", KeyLast4: "ABCD", Dim: 1024},
		Rerank:    setting.Provider{URL: "https://rerank.example", Model: "bge-reranker", APIKey: "rk-key-9876", KeyLast4: "9876"},
	}
	if got != want || s.Current() != want {
		t.Fatalf("Update() = %+v\nCurrent() = %+v\nwant %+v", got, s.Current(), want)
	}
	if len(tr.calls) != 1 || tr.calls[0].Embedding.APIKey != "sk-embed-ABCD" {
		t.Fatalf("tester calls = %+v", tr.calls)
	}
	raw := string(st.m["search"])
	for _, k := range []string{"sk-embed-ABCD", "rk-key-9876"} {
		if strings.Contains(raw, k) {
			t.Fatalf("stored JSON contains plaintext key %q: %s", k, raw)
		}
	}
	for _, f := range []string{`"api_key_enc"`, `"api_key_last4":"ABCD"`, `"dim":1024`} {
		if !strings.Contains(raw, f) {
			t.Fatalf("stored JSON missing %s: %s", f, raw)
		}
	}
	if again := newSvc(t, st, c, tr.test).Current(); again != want {
		t.Fatalf("reloaded = %+v, want %+v", again, want)
	}
}

func TestService_UpdateKeyNilKeepsEmptyDeletes(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t, &memStore{}, newCipher(t), (&okTester{}).test)
	if _, err := s.Update(ctx, embOn, rerOn); err != nil {
		t.Fatal(err)
	}
	keep := setting.ProviderUpdate{URL: embOn.URL, Model: "other"}
	del := setting.ProviderUpdate{URL: rerOn.URL, Model: rerOn.Model, APIKey: ptr("")}
	got, err := s.Update(ctx, keep, del)
	if err != nil {
		t.Fatal(err)
	}
	if got.Embedding.APIKey != "sk-embed-ABCD" || got.Embedding.KeyLast4 != "ABCD" || got.Embedding.Model != "other" {
		t.Fatalf("nil APIKey did not keep key: %+v", got.Embedding)
	}
	if got.Rerank.APIKey != "" || got.Rerank.KeyLast4 != "" {
		t.Fatalf(`"" APIKey did not delete key: %+v`, got.Rerank)
	}
}

func TestService_UpdateTesterErrorSavesNothing(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	ok := &okTester{}
	fail := false
	s := newSvc(t, st, newCipher(t), func(ctx context.Context, x setting.Search) (setting.Search, error) {
		if fail {
			return x, errors.New("401 invalid api key")
		}
		return ok.test(ctx, x)
	})
	before, err := s.Update(ctx, embOn, rerOn)
	if err != nil {
		t.Fatal(err)
	}
	stored := string(st.m["search"])
	fail = true
	_, err = s.Update(ctx, setting.ProviderUpdate{URL: "http://other", Model: "m", APIKey: ptr("new-key")}, rerOn)
	if !errors.Is(err, setting.ErrUpstream) || !strings.Contains(err.Error(), "401 invalid api key") {
		t.Fatalf("err = %v, want ErrUpstream with provider message", err)
	}
	if strings.Contains(err.Error(), "new-key") {
		t.Fatal("error leaks key")
	}
	if s.Current() != before || string(st.m["search"]) != stored {
		t.Fatal("failed Update changed state")
	}
	if err := s.Test(ctx, embOn, rerOn); !errors.Is(err, setting.ErrUpstream) {
		t.Fatalf("Test err = %v", err)
	}
}

func TestService_DisableAllSkipsTesterAndDropsKeys(t *testing.T) {
	ctx := context.Background()
	tr := &okTester{}
	s := newSvc(t, &memStore{}, newCipher(t), tr.test)
	if _, err := s.Update(ctx, embOn, rerOn); err != nil {
		t.Fatal(err)
	}
	got, err := s.Update(ctx, setting.ProviderUpdate{}, setting.ProviderUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if got != (setting.Search{}) {
		t.Fatalf("disabled config = %+v, want zero", got)
	}
	if len(tr.calls) != 1 {
		t.Fatalf("tester called %d times, want 1", len(tr.calls))
	}
	if err := s.Test(ctx, setting.ProviderUpdate{}, setting.ProviderUpdate{}); err != nil {
		t.Fatal(err)
	}
}

func TestService_Validation(t *testing.T) {
	ctx := context.Background()
	tr := &okTester{}
	s := newSvc(t, &memStore{}, newCipher(t), tr.test)
	for _, u := range []setting.ProviderUpdate{
		{URL: "ftp://x", Model: "m"},
		{URL: "http://", Model: "m"},
		{URL: "not a url", Model: "m"},
		{URL: "http://x", Model: "  "},
	} {
		if _, err := s.Update(ctx, u, setting.ProviderUpdate{}); !errors.Is(err, setting.ErrInvalidArgument) {
			t.Errorf("Update(%+v) err = %v, want ErrInvalidArgument", u, err)
		}
		if err := s.Test(ctx, setting.ProviderUpdate{}, u); !errors.Is(err, setting.ErrInvalidArgument) {
			t.Errorf("Test(rerank %+v) err = %v, want ErrInvalidArgument", u, err)
		}
	}
	if len(tr.calls) != 0 {
		t.Fatal("tester called for invalid input")
	}
}

func TestService_LostKeyFile(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	a := newSvc(t, st, newCipher(t), (&okTester{}).test)
	if _, err := a.Update(ctx, embOn, setting.ProviderUpdate{}); err != nil {
		t.Fatal(err)
	}
	tr := &okTester{}
	b, err := setting.NewService(ctx, st, newCipher(t), tr.test)
	if err != nil {
		t.Fatalf("NewService with another key: %v", err)
	}
	cur := b.Current()
	if cur.Embedding.APIKey != "" || cur.Embedding.KeyLast4 != "" || cur.Embedding.URL != embOn.URL {
		t.Fatalf("Current().Embedding = %+v", cur.Embedding)
	}
	if !slices.Contains(b.Warnings(), "api_key_unreadable:embedding") {
		t.Fatalf("Warnings() = %v", b.Warnings())
	}
	// Re-entering the key clears the warning.
	if _, err := b.Update(ctx, setting.ProviderUpdate{URL: embOn.URL, Model: "bge-m3", APIKey: ptr("fresh-key")}, setting.ProviderUpdate{}); err != nil {
		t.Fatal(err)
	}
	if len(b.Warnings()) != 0 {
		t.Fatalf("Warnings() after re-entering key = %v", b.Warnings())
	}
}

func TestService_OnChange(t *testing.T) {
	s := newSvc(t, &memStore{}, newCipher(t), (&okTester{}).test)
	var got []setting.Search
	s.OnChange(func(x setting.Search) {
		_ = s.Current() // must not deadlock: callbacks run outside the lock
		got = append(got, x)
	})
	want, err := s.Update(context.Background(), embOn, setting.ProviderUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("OnChange got %+v", got)
	}
}

func TestService_ConcurrentCurrentAndUpdate(t *testing.T) {
	s := newSvc(t, &memStore{}, newCipher(t), (&okTester{}).test)
	s.OnChange(func(setting.Search) {})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m := embOn
			m.Model = strings.Repeat("m", i+1)
			if _, err := s.Update(context.Background(), m, rerOn); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			_ = s.Current()
			_ = s.Warnings()
		}()
	}
	wg.Wait()
}

func TestService_KeyNotKeptAcrossHostChange(t *testing.T) {
	ctx := context.Background()
	tr := &okTester{}
	s := newSvc(t, &memStore{}, newCipher(t), tr.test)
	if _, err := s.Update(ctx, embOn, rerOn); err != nil {
		t.Fatal(err)
	}
	before := s.Current()
	calls := len(tr.calls)

	for _, u := range []string{"http://evil.example/v1", "https://embed:80/v1", "http://embed:81/v1"} {
		_, err := s.Update(ctx, setting.ProviderUpdate{URL: u, Model: "m"}, setting.ProviderUpdate{})
		if !errors.Is(err, setting.ErrInvalidArgument) || !strings.Contains(err.Error(), "re-enter the API key when changing the provider host") {
			t.Fatalf("Update(%s, nil key) err = %v", u, err)
		}
		if err := s.Test(ctx, setting.ProviderUpdate{URL: u, Model: "m"}, setting.ProviderUpdate{}); !errors.Is(err, setting.ErrInvalidArgument) {
			t.Fatalf("Test(%s, nil key) err = %v", u, err)
		}
	}
	if len(tr.calls) != calls || s.Current() != before {
		t.Fatal("host change with kept key reached the tester or changed state")
	}

	// Same host, different path: key kept.
	got, err := s.Update(ctx, setting.ProviderUpdate{URL: "http://embed:80/other?x=1", Model: "m"}, rerOn)
	if err != nil || got.Embedding.APIKey != "sk-embed-ABCD" {
		t.Fatalf("same host: %+v, %v", got.Embedding, err)
	}
	// Host change with a new key: OK.
	got, err = s.Update(ctx, setting.ProviderUpdate{URL: "http://new-host", Model: "m", APIKey: ptr("new-key-12")}, rerOn)
	if err != nil || got.Embedding.APIKey != "new-key-12" {
		t.Fatalf("new host + new key: %+v, %v", got.Embedding, err)
	}
	// Host change when no key is stored: OK.
	if _, err := s.Update(ctx, setting.ProviderUpdate{URL: "http://new-host", Model: "m", APIKey: ptr("")}, rerOn); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, setting.ProviderUpdate{URL: "http://third-host", Model: "m"}, rerOn); err != nil {
		t.Fatalf("host change without stored key: %v", err)
	}
}

func TestService_KeyLast4OnlyForLongKeys(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s := newSvc(t, st, newCipher(t), (&okTester{}).test)
	for key, want := range map[string]string{"abcdefg": "", "abcdefgh": "efgh"} {
		got, err := s.Update(ctx, setting.ProviderUpdate{URL: "http://e", Model: "m", APIKey: ptr(key)}, setting.ProviderUpdate{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Embedding.KeyLast4 != want || !strings.Contains(string(st.m["search"]), `"api_key_last4":"`+want+`"`) {
			t.Fatalf("key %q: KeyLast4 = %q, stored %s; want %q", key, got.Embedding.KeyLast4, st.m["search"], want)
		}
	}
}
