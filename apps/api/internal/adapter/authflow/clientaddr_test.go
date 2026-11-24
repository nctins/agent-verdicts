package authflow_test

import (
	"net/http/httptest"
	"testing"

	"agent-verdicts/internal/adapter/authflow"
)

func TestClientAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/setup", nil)
	r.RemoteAddr = "172.18.0.5:41000"
	if got := authflow.ClientAddr(r); got != "172.18.0.5:41000" {
		t.Errorf("no header: %q", got)
	}
	r.Header.Set("X-Real-IP", " 203.0.113.7 ")
	if got := authflow.ClientAddr(r); got != "203.0.113.7" {
		t.Errorf("X-Real-IP: %q", got)
	}
	r.Header.Set("X-Real-IP", "not an ip")
	if got := authflow.ClientAddr(r); got != "172.18.0.5:41000" {
		t.Errorf("garbage header must fall back to RemoteAddr: %q", got)
	}
}
