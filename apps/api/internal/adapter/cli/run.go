package cli

import (
	"context"
	"fmt"
	"io"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/signal"
)

// Deps are the repositories the commands read from; the caller opens them.
type Deps struct {
	Contributions contribution.Repository
	Signals       signal.Repository
	Actors        actor.Repository
}

// Run dispatches "list" or "show" (args[0]); the rest are that command's
// flags. Exit codes: 0 ok, 2 usage, 1 runtime.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: agentverdicts <list|show> [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return RunList(ctx, args[1:], stdout, stderr, deps)
	case "show":
		return RunShow(ctx, args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "usage: agentverdicts <list|show> [flags] (unknown subcommand %q)\n", args[0])
		return 2
	}
}
