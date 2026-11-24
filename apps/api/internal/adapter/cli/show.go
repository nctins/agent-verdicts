package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"
)

// RunShow prints one contribution with its signals (claims), answers
// (questions) or the question it answers.
func RunShow(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectID := fs.String("project", "", "project ID (required)")
	id := fs.String("id", "", "contribution ID (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := project.ValidateID(*projectID); err != nil || *id == "" {
		fmt.Fprintln(stderr, "show: -project (valid) and -id are required")
		return 2
	}
	c, err := deps.Contributions.Get(ctx, *projectID, *id)
	if errors.Is(err, contribution.ErrNotFound) {
		fmt.Fprintf(stderr, "show: contribution %q not found in project %q\n", oneLine(*id), oneLine(*projectID))
		return 1
	} else if err != nil {
		fmt.Fprintf(stderr, "show: %v\n", err)
		return 1
	}
	ref, err := view.NewReference(ctx, deps.Contributions, deps.Signals, deps.Actors, c, true) // the CLI is the operator: it sees hidden items too
	if err != nil {
		fmt.Fprintf(stderr, "show: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\nid: %s\nkind: %s\nstatus: %s\nactor: %s\ncreated_at: %s\n", oneLine(ref.Title), ref.ID, ref.Kind, statusText(ref.Status), oneLine(ref.ActorName), ref.CreatedAt)
	if ref.AnswerCount != nil {
		fmt.Fprintf(stdout, "answers: %d\n", *ref.AnswerCount)
	}
	if ref.AnswerTo != nil {
		fmt.Fprintf(stdout, "answers question: %s %s\n", ref.AnswerTo.ID, oneLine(ref.AnswerTo.Title))
	}
	fmt.Fprintf(stdout, "\n%s\n", block(ref.Body))

	if c.Kind == contribution.KindQuestion {
		fmt.Fprintln(stdout, "\n== answers ==")
		for _, a := range ref.Answers {
			st := ""
			if a.State != nil {
				st = string(*a.State)
			}
			fmt.Fprintf(stdout, "%s %s %s\n", a.ID, st, oneLine(a.Title))
		}
		return 0
	}
	g := ref.Signals
	for _, grp := range []struct {
		name  string
		items []view.SignalItem
	}{{"favorable", g.Favorable}, {"unfavorable", g.Unfavorable}, {"neutral", g.Neutral}, {"history", g.History}} {
		fmt.Fprintf(stdout, "\n== %s (%d) ==\n", grp.name, len(grp.items))
		for _, s := range grp.items {
			mark := ""
			if s.Superseded {
				mark = " (superseded)"
			}
			fmt.Fprintf(stdout, "%s  %s  %s  %s  %s\n  %s%s\n", s.Stance, s.Kind, oneLine(s.ActorName), s.CreatedAt, s.ID, block(s.Body), mark)
		}
	}
	return 0
}
