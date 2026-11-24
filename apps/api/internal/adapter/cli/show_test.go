package cli_test

import (
	"context"
	"strings"
	"testing"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/signal"
)

func TestShow_ClaimGroupsSignalsAndHistory(t *testing.T) {
	f := newFx(t)
	c := f.contribution(contribution.KindClaim, "the claim", "")
	f.signal(c, signal.KindAssessment, signal.StanceUnfavorable, "old take")
	f.signal(c, signal.KindAssessment, signal.StanceFavorable, "new take")
	f.signal(c, signal.KindChallenge, signal.StanceNeutral, "a challenge")

	code, out, e := run(f, "show", "-project", "p", "-id", c.ID)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, e)
	}
	for _, want := range []string{"the claim", "claim", "body of the claim", "favorable", "neutral", "assessment", "challenge",
		"Alice Agent", "new take", "a challenge", "history", "old take (superseded)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old take") && strings.Index(out, "old take") < strings.Index(out, "history") {
		t.Errorf("superseded signal listed before history:\n%s", out)
	}
}

func TestShow_QuestionAndAnswer(t *testing.T) {
	f := newFx(t)
	q := f.contribution(contribution.KindQuestion, "why?", "")
	a := f.contribution(contribution.KindClaim, "because", q.ID)

	_, out, _ := run(f, "show", "-project", "p", "-id", q.ID)
	if !strings.Contains(out, "answers: 1") || !strings.Contains(out, a.ID+" InsufficientEvidence because") {
		t.Errorf("question output:\n%s", out)
	}
	_, out, _ = run(f, "show", "-project", "p", "-id", a.ID)
	if !strings.Contains(out, "answers question: "+q.ID+" why?") {
		t.Errorf("answer output:\n%s", out)
	}
}

func TestShow_Errors(t *testing.T) {
	f := newFx(t)
	if code, _, e := run(f, "show", "-project", "p", "-id", "nope"); code != 1 || e == "" {
		t.Errorf("not found: code=%d stderr=%q", code, e)
	}
	if code, _, _ := run(f, "show", "-project", "p"); code != 2 {
		t.Errorf("missing -id code=%d", code)
	}
}

func TestOutput_StripsControlCharacters(t *testing.T) {
	f := newFx(t)
	evil := "x\x1b]0;pwned\x07\x1b[2Jy\u009b"
	c, err := contribution.Create(context.Background(), f.projects, f.deps.Actors, f.deps.Contributions,
		contribution.NewInput{ProjectID: "p", ActorID: f.actor.ID, Kind: contribution.KindClaim, Title: "t" + evil + "a\nb", Body: "l1\n" + evil})
	if err != nil {
		t.Fatal(err)
	}
	f.signal(c, signal.KindChallenge, signal.StanceNeutral, evil)
	_, list, _ := run(f, "list", "-project", "p")
	_, show, _ := run(f, "show", "-project", "p", "-id", c.ID)
	for _, out := range []string{list, show} {
		if strings.ContainsAny(out, "\x1b\x07\u009b") {
			t.Errorf("control bytes in output: %q", out)
		}
	}
	if n := strings.Count(list, "\n"); n != 1 {
		t.Errorf("title newline not collapsed: %q", list)
	}
}
