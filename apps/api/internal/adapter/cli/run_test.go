package cli_test

import "testing"

func TestRun_UsageErrors(t *testing.T) {
	f := newFx(t)
	if code, _, _ := run(f); code != 2 {
		t.Errorf("no args code=%d", code)
	}
	if code, _, _ := run(f, "bogus"); code != 2 {
		t.Errorf("unknown code=%d", code)
	}
}
