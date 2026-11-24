package setting_test

import (
	"os"
	"path/filepath"
	"testing"

	"agent-verdicts/internal/setting"
)

func TestCipher_SealOpenRoundtrip(t *testing.T) {
	c, err := setting.LoadKey(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.Seal("sk-secret-1234")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "sk-secret-1234" {
		t.Fatal("Seal returned plaintext")
	}
	got, err := c.Open(enc)
	if err != nil || got != "sk-secret-1234" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	enc2, _ := c.Seal("sk-secret-1234")
	if enc2 == enc {
		t.Fatal("Seal is deterministic; nonce must be random")
	}
}

func TestCipher_OpenWithOtherKeyFails(t *testing.T) {
	a, _ := setting.LoadKey(t.TempDir())
	b, _ := setting.LoadKey(t.TempDir())
	enc, _ := a.Seal("x")
	if _, err := b.Open(enc); err == nil {
		t.Fatal("Open with another key succeeded")
	}
	if _, err := a.Open("not base64!"); err == nil {
		t.Fatal("Open of garbage succeeded")
	}
	if _, err := a.Open(""); err == nil {
		t.Fatal("Open of empty succeeded")
	}
}

func TestLoadKey_CreatesFileOnceWithMode0600(t *testing.T) {
	dir := t.TempDir()
	a, err := setting.LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "settings.key"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 || fi.Size() != 32 {
		t.Fatalf("mode=%v size=%d, want 0600 and 32", fi.Mode().Perm(), fi.Size())
	}
	enc, _ := a.Seal("hello")
	b, err := setting.LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Open(enc); err != nil || got != "hello" {
		t.Fatalf("second LoadKey read a different key: %q, %v", got, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("dir has %d entries, want only settings.key", len(entries))
	}
}

func TestLoadKey_WrongSizeIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := setting.LoadKey(dir); err == nil {
		t.Fatal("LoadKey accepted a 5-byte key file")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.key")); string(b) != "short" {
		t.Fatal("LoadKey overwrote an existing key file")
	}
}
