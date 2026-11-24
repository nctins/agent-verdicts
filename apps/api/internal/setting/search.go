// Package setting holds runtime configuration stored in the setting table,
// currently the search providers (spec §6.6).
package setting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
)

var (
	ErrNotFound        = errors.New("setting: not found")
	ErrInvalidArgument = errors.New("setting: invalid argument")
	ErrUpstream        = errors.New("setting: upstream failed")
)

const searchKey = "search"

// Provider is one search provider. URL == "" means disabled. APIKey is
// plaintext and lives only in memory.
type Provider struct {
	URL, Model string
	APIKey     string
	KeyLast4   string
	Dim        int // embedding only
}

type Search struct{ Embedding, Rerank Provider }

// Status is filled by the search service; it is only a data type here.
type Status struct {
	Pipeline                  string
	EmbeddedCount, TotalCount int
	Reindexing                bool
	Warnings                  []string
}

// Store persists raw JSON values by key. Get returns ErrNotFound when absent.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, v []byte) error
}

// Tester checks the enabled providers of a resolved candidate (plaintext
// keys) and returns it with Embedding.Dim filled. It must skip disabled
// providers (URL == "").
type Tester func(ctx context.Context, s Search) (Search, error)

// ProviderUpdate: APIKey nil keeps the current key, "" deletes it, anything
// else replaces it.
type ProviderUpdate struct {
	URL, Model string
	APIKey     *string
}

type storedProvider struct {
	URL         string `json:"url"`
	Model       string `json:"model"`
	APIKeyEnc   string `json:"api_key_enc"`
	APIKeyLast4 string `json:"api_key_last4"`
}

type storedSearch struct {
	Embedding struct {
		storedProvider
		Dim int `json:"dim"`
	} `json:"embedding"`
	Rerank storedProvider `json:"rerank"`
}

type Service struct {
	store  Store
	cipher *Cipher
	test   Tester

	updateMu sync.Mutex // serializes Update so a slow test cannot interleave writes

	mu        sync.Mutex
	cur       Search
	warnings  []string
	listeners []func(Search)
}

// NewService loads setting['search']. Keys that no longer decrypt (lost key
// file) load as "no key" with a warning instead of failing.
func NewService(ctx context.Context, store Store, cipher *Cipher, test Tester) (*Service, error) {
	s := &Service{store: store, cipher: cipher, test: test}
	raw, err := store.Get(ctx, searchKey)
	if errors.Is(err, ErrNotFound) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("setting: load search: %w", err)
	}
	var st storedSearch
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("setting: decode search: %w", err)
	}
	s.cur.Embedding = s.open("embedding", st.Embedding.storedProvider)
	s.cur.Embedding.Dim = st.Embedding.Dim
	s.cur.Rerank = s.open("rerank", st.Rerank)
	return s, nil
}

func (s *Service) open(name string, p storedProvider) Provider {
	out := Provider{URL: p.URL, Model: p.Model}
	if p.APIKeyEnc == "" {
		return out
	}
	key, err := s.cipher.Open(p.APIKeyEnc)
	if err != nil {
		s.warnings = append(s.warnings, "api_key_unreadable:"+name)
		return out
	}
	out.APIKey, out.KeyLast4 = key, p.APIKeyLast4
	return out
}

func (s *Service) Current() Search {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *Service) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.warnings)
}

func (s *Service) OnChange(fn func(Search)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// Test validates and checks a candidate without saving it.
func (s *Service) Test(ctx context.Context, emb, rer ProviderUpdate) error {
	_, err := s.check(ctx, emb, rer)
	return err
}

// Update tests the candidate, then saves it and makes it current. On any
// error nothing changes.
func (s *Service) Update(ctx context.Context, emb, rer ProviderUpdate) (Search, error) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	cand, err := s.check(ctx, emb, rer)
	if err != nil {
		return Search{}, err
	}
	raw, err := s.encode(cand)
	if err != nil {
		return Search{}, err
	}
	if err := s.store.Put(ctx, searchKey, raw); err != nil {
		return Search{}, fmt.Errorf("setting: save search: %w", err)
	}

	s.mu.Lock()
	s.cur = cand
	s.warnings = slices.DeleteFunc(s.warnings, func(w string) bool {
		return (w == "api_key_unreadable:embedding" && keyTouched(emb)) ||
			(w == "api_key_unreadable:rerank" && keyTouched(rer))
	})
	listeners := slices.Clone(s.listeners)
	s.mu.Unlock()

	for _, fn := range listeners {
		fn(cand)
	}
	return cand, nil
}

// keyTouched reports whether an update sets, deletes or (by disabling) drops the key.
func keyTouched(u ProviderUpdate) bool { return u.APIKey != nil || strings.TrimSpace(u.URL) == "" }

// check resolves the candidate against the current config and runs the
// tester when at least one provider is enabled.
func (s *Service) check(ctx context.Context, emb, rer ProviderUpdate) (Search, error) {
	cur := s.Current()
	var cand Search
	var err error
	if cand.Embedding, err = resolve("embedding", cur.Embedding, emb); err != nil {
		return Search{}, err
	}
	if cand.Rerank, err = resolve("rerank", cur.Rerank, rer); err != nil {
		return Search{}, err
	}
	if cand.Embedding.URL == "" && cand.Rerank.URL == "" {
		return cand, nil
	}
	tested, err := s.test(ctx, cand)
	if err != nil {
		// Providers may echo the key back; never let it reach an API response.
		msg := err.Error()
		for _, k := range []string{cand.Embedding.APIKey, cand.Rerank.APIKey} {
			if k != "" {
				msg = strings.ReplaceAll(msg, k, "[redacted]")
			}
		}
		return Search{}, fmt.Errorf("%w: %s", ErrUpstream, msg)
	}
	if cand.Embedding.URL != "" {
		cand.Embedding.Dim = tested.Embedding.Dim
	}
	return cand, nil
}

func resolve(name string, cur Provider, u ProviderUpdate) (Provider, error) {
	p := Provider{URL: strings.TrimSpace(u.URL), Model: strings.TrimSpace(u.Model)}
	if p.URL == "" {
		return Provider{}, nil
	}
	parsed, err := url.Parse(p.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Provider{}, fmt.Errorf("%w: %s url must be http(s)://host", ErrInvalidArgument, name)
	}
	if p.Model == "" {
		return Provider{}, fmt.Errorf("%w: %s model is required", ErrInvalidArgument, name)
	}
	if u.APIKey != nil {
		p.APIKey = strings.TrimSpace(*u.APIKey)
	} else if cur.APIKey != "" {
		// A kept key is sent as a bearer token to the new URL, so it may only
		// follow the provider within the same origin (scheme + host + port).
		old, _ := url.Parse(cur.URL) // stored URLs were validated on save
		if old == nil || old.Scheme != parsed.Scheme || !strings.EqualFold(old.Host, parsed.Host) {
			return Provider{}, fmt.Errorf("%w: %s: re-enter the API key when changing the provider host", ErrInvalidArgument, name)
		}
		p.APIKey = cur.APIKey
	}
	p.KeyLast4 = last4(p.APIKey)
	return p, nil
}

// last4 is the display hint for a key; keys shorter than 8 runes get none,
// since 4 runes would reveal too much of them.
func last4(key string) string {
	r := []rune(key)
	if len(r) < 8 {
		return ""
	}
	return string(r[len(r)-4:])
}

func (s *Service) encode(cand Search) ([]byte, error) {
	var st storedSearch
	var err error
	if st.Embedding.storedProvider, err = s.seal(cand.Embedding); err != nil {
		return nil, err
	}
	st.Embedding.Dim = cand.Embedding.Dim
	if st.Rerank, err = s.seal(cand.Rerank); err != nil {
		return nil, err
	}
	return json.Marshal(st)
}

func (s *Service) seal(p Provider) (storedProvider, error) {
	out := storedProvider{URL: p.URL, Model: p.Model, APIKeyLast4: p.KeyLast4}
	if p.APIKey == "" {
		return out, nil
	}
	enc, err := s.cipher.Seal(p.APIKey)
	if err != nil {
		return storedProvider{}, fmt.Errorf("setting: seal key: %w", err)
	}
	out.APIKeyEnc = enc
	return out, nil
}
