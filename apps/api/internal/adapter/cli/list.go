package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode"

	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/trust"
)

type stateFlags []trust.State

func (s *stateFlags) String() string { return "" }
func (s *stateFlags) Set(v string) error {
	switch st := trust.State(v); st {
	case trust.StateFavorable, trust.StateUnfavorable, trust.StateConflict, trust.StateInsufficientEvidence:
		*s = append(*s, st)
		return nil
	}
	return fmt.Errorf("invalid state %q (Favorable, Unfavorable, Conflict, InsufficientEvidence)", v)
}

// statusText is the state for a claim, the question status for a question.
func statusText(st view.Status) string {
	if st.State != nil {
		return string(*st.State)
	}
	return st.QuestionStatus
}

// RunList prints one line per contribution of a project, newest first,
// across all pages: "<id>  <kind>  <state|question status>  <title>".
func RunList(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectID := fs.String("project", "", "project ID (required)")
	kind := fs.String("kind", "", "claim or question")
	var states stateFlags
	fs.Var(&states, "state", "claim state filter; repeatable")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := project.ValidateID(*projectID); err != nil {
		fmt.Fprintf(stderr, "list: -project: %s\n", oneLine(err.Error()))
		return 2
	}
	k := contribution.Kind(*kind)
	if k != "" && k != contribution.KindClaim && k != contribution.KindQuestion {
		fmt.Fprintf(stderr, "list: invalid -kind %q (claim, question)\n", *kind)
		return 2
	}
	f := contribution.ListFilter{Kind: k, States: states, Limit: contribution.MaxLimit, IncludeSuperseded: true, IncludeHidden: true}
	for {
		page, err := deps.Contributions.List(ctx, *projectID, f)
		if err != nil {
			fmt.Fprintf(stderr, "list: %v\n", err)
			return 1
		}
		var qids []string
		for _, c := range page.Items {
			if c.Kind == contribution.KindQuestion {
				qids = append(qids, c.ID)
			}
		}
		var info map[string]contribution.QuestionInfo
		if len(qids) > 0 {
			var err error
			if info, err = deps.Contributions.QuestionInfo(ctx, qids); err != nil {
				fmt.Fprintf(stderr, "list: %v\n", err)
				return 1
			}
		}
		for _, c := range page.Items {
			var q *contribution.QuestionInfo
			if i, ok := info[c.ID]; ok {
				q = &i
			}
			fmt.Fprintf(stdout, "%s  %s  %s  %s\n", c.ID, c.Kind, statusText(view.StatusOf(c.Kind, c.State(), q)), oneLine(c.Title))
		}
		if page.NextCursor == "" {
			return 0
		}
		if page.NextCursor == f.Cursor {
			fmt.Fprintln(stderr, "list: pagination cursor did not advance")
			return 1
		}
		f.Cursor = page.NextCursor
	}
}

// clean replaces every control rune (ESC, C1, DEL, ...) except \n and \t
// with U+FFFD, so agent-authored text cannot drive the operator's terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return '\uFFFD'
		}
		return r
	}, s)
}

// oneLine sanitizes s and keeps it on its own output line.
func oneLine(s string) string { return strings.Join(strings.Fields(clean(s)), " ") }

// block sanitizes multi-line text and indents continuation lines.
func block(s string) string { return strings.ReplaceAll(clean(s), "\n", "\n  ") }
