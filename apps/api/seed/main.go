// Command seed loads seed/demo.json into a development database:
//
//	go run ./seed [-database-url URL]   (default $AGENTVERDICTS_DATABASE_URL)
//
// A project that already has contributions is skipped, so running it again
// adds nothing. The admin password is generated and printed once, when the
// admin is created. Dev only: not part of the agentverdicts binary.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/signal"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed demo.json
var demoJSON []byte

type demo struct {
	Actors []struct {
		Key, Type, Name, Username string
	} `json:"actors"`
	Contributions []struct {
		Key, Project, Actor, Kind, Title, Body, Answers string
	} `json:"contributions"`
	Signals []struct {
		Contribution, Actor, Kind, Stance, Body string
	} `json:"signals"`
}

func parse(data []byte) (demo, error) {
	var d demo
	err := json.Unmarshal(data, &d)
	return d, err
}

func main() {
	url := flag.String("database-url", os.Getenv("AGENTVERDICTS_DATABASE_URL"), "Postgres URL")
	flag.Parse()
	if *url == "" {
		log.Fatal("seed: -database-url or AGENTVERDICTS_DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := postgres.Open(ctx, *url)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		log.Fatalf("seed: %v", err)
	}
	if err := seed(ctx, pool, demoJSON, os.Stdout); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func seed(ctx context.Context, pool *pgxpool.Pool, data []byte, out io.Writer) error {
	d, err := parse(data)
	if err != nil {
		return fmt.Errorf("parse demo.json: %w", err)
	}
	actors, logins := postgres.NewActorRepo(pool), postgres.NewLoginRepo(pool)
	projects, contributions, signals := postgres.NewProjectRepo(pool), postgres.NewContributionRepo(pool), postgres.NewSignalRepo(pool)

	counts, err := contributions.CountByProject(ctx)
	if err != nil {
		return err
	}
	todo := map[string]bool{}
	for _, c := range d.Contributions {
		if counts[c.Project].Contributions == 0 {
			todo[c.Project] = true
		}
	}
	if len(todo) == 0 {
		fmt.Fprintln(out, "seed: every demo project already has contributions; nothing to do")
		return nil
	}

	actorID := map[string]string{}
	existing, err := actors.List(ctx)
	if err != nil {
		return err
	}
	for _, a := range d.Actors {
		if a.Type == string(actor.TypeHuman) {
			l, err := logins.GetByUsername(ctx, a.Username)
			if err == nil {
				actorID[a.Key] = l.ActorID
				continue
			}
			if !errors.Is(err, authentication.ErrLoginNotFound) {
				return err
			}
			h := actor.New(actor.TypeHuman)
			h.Name = a.Name
			if err := actors.Create(ctx, h); err != nil {
				return err
			}
			if err := access.GrantAdmin(ctx, postgres.NewAccessRepo(pool), h.ID); err != nil {
				return err
			}
			pw, err := authentication.SetLogin(ctx, logins, actors, h.ID, a.Username, "", true)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "seed: created admin %q, temporary password (shown once): %s\n", a.Username, pw)
			actorID[a.Key] = h.ID
			continue
		}
		for _, e := range existing { // agents are matched by name
			if e.Type == actor.TypeAgent && e.Name == a.Name {
				actorID[a.Key] = e.ID
			}
		}
		if actorID[a.Key] == "" {
			ag := actor.New(actor.TypeAgent)
			ag.Name = a.Name
			if err := actors.Create(ctx, ag); err != nil {
				return err
			}
			actorID[a.Key] = ag.ID
		}
	}

	for id := range todo {
		if _, err := project.Create(ctx, projects, id, "", ""); err != nil && !errors.Is(err, project.ErrAlreadyExists) {
			return fmt.Errorf("project %s: %w", id, err)
		}
	}
	ids, project := map[string]string{}, map[string]string{} // demo key -> contribution id, project
	n := 0
	for _, c := range d.Contributions {
		if !todo[c.Project] {
			continue
		}
		created, err := contribution.Create(ctx, projects, actors, contributions, contribution.NewInput{
			ProjectID: c.Project, ActorID: actorID[c.Actor], Kind: contribution.Kind(c.Kind),
			Title: c.Title, Body: c.Body, AnswersID: ids[c.Answers],
		})
		if err != nil {
			return fmt.Errorf("contribution %s: %w", c.Key, err)
		}
		ids[c.Key], project[c.Key] = created.ID, c.Project
		n++
	}
	s := 0
	for _, sg := range d.Signals {
		cid, ok := ids[sg.Contribution]
		if !ok {
			continue // its project was skipped
		}
		if _, err := signal.Create(ctx, contributions, actors, signals, signal.NewInput{
			ProjectID: project[sg.Contribution], ContributionID: cid, ActorID: actorID[sg.Actor],
			Kind: signal.Kind(sg.Kind), Stance: signal.Stance(sg.Stance), Body: sg.Body,
		}); err != nil {
			return fmt.Errorf("signal on %s: %w", sg.Contribution, err)
		}
		s++
	}
	fmt.Fprintf(out, "seed: %d contributions and %d signals in %d project(s)\n", n, s, len(todo))
	return nil
}
