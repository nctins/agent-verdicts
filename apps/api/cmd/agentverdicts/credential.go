package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

// runCredentialCLI opens the database (AGENTVERDICTS_DATABASE_URL, see
// openDB in main.go), builds the actor and credential repositories, and
// dispatches to the operator-only subcommand. This path runs entirely
// outside the authenticated network boundary: it never goes through HTTP or
// MCP and never reads an Authorization header. Database access is full
// access, so it is the recovery path when nobody can log in: an operator
// with exec access to the container or host runs it.
func runCredentialCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printCredentialUsage(stderr)
		return 2
	}
	if isHelpArg(args[0]) {
		printCredentialUsage(stdout)
		return 0
	}

	dispatch := func(actors actor.Repository, credentials authentication.Repository, logins authentication.LoginRepository, acc access.Repository, audits audit.Repository) int {
		switch args[0] {
		case "bootstrap":
			return runCredentialBootstrap(ctx, args[1:], stdout, stderr, actors, credentials, logins, acc, audits)
		case "password":
			return runCredentialPassword(ctx, args[1:], stdout, stderr, actors, logins, audits)
		case "issue":
			return runCredentialIssue(ctx, args[1:], stdout, stderr, actors, credentials, audits)
		case "revoke":
			return runCredentialRevoke(ctx, args[1:], stdout, stderr, credentials, audits)
		default:
			fmt.Fprintf(stderr, "agentverdicts credential: unknown subcommand %q\n", args[0])
			printCredentialUsage(stderr)
			return 2
		}
	}

	// `credential <sub> -h` only needs flag parsing, which every
	// subcommand does before touching a repository — skip the database.
	if hasHelpArg(args) {
		return dispatch(nil, nil, nil, nil, nil)
	}

	pool, err := openDB(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "agentverdicts: open database: %v\n", err)
		return 1
	}
	defer pool.Close()

	return dispatch(postgres.NewActorRepo(pool), postgres.NewCredentialRepo(pool), postgres.NewLoginRepo(pool), postgres.NewAccessRepo(pool), postgres.NewAuditRepo(pool))
}

func printCredentialUsage(w io.Writer) {
	fmt.Fprint(w, `usage: agentverdicts credential <bootstrap|issue|revoke> [flags]

  bootstrap -actor-type Human -name <name> -username <u> [-admin]
                                               create a person with a temporary login password
  bootstrap -actor-type Agent -name <name>     create an agent and its MCP token
  password  -username <u> [-actor <actor-id>]  set a new temporary login password
                                               (-actor also assigns the username)
  issue     -actor <actor-id> [-label <text>] [-expires-in-days 30|90|365|0]
                                               issue another credential for an existing Actor
                                               (default expiry 90 days; 0 = never; bootstrap tokens never expire)
  revoke    -credential <credential-id>        revoke a credential (the id, not the secret)

Run "agentverdicts credential <subcommand> -h" for a subcommand's flags.
`)
}

// runCredentialBootstrap creates an Actor and its first credential. With
// -username (Human only) it also sets a temporary login password, so the
// first admin can log in with a username instead of pasting a token.
func runCredentialBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer, actors actor.Repository, credentials authentication.Repository, logins authentication.LoginRepository, acc access.Repository, audits audit.Repository) int {
	fs := flag.NewFlagSet("credential bootstrap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	actorType := fs.String("actor-type", "", "Actor type for the newly created Actor: 'Human' or 'Agent' (required)")
	rawName := fs.String("name", "", "display name for the newly created Actor, e.g. 'Alice' or 'Review Bot' (required)")
	rawUsername := fs.String("username", "", "login username (required for Human actors); a temporary password is printed")
	admin := fs.Bool("admin", false, "give the newly created Actor the admin role on every project — only valid with -actor-type Human")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	name, err := actor.NormalizeName(*rawName)
	if err != nil {
		fmt.Fprintf(stderr, "credential bootstrap: -name: %v\n", err)
		return 2
	}

	var t actor.Type
	switch *actorType {
	case string(actor.TypeHuman):
		t = actor.TypeHuman
	case string(actor.TypeAgent):
		t = actor.TypeAgent
	default:
		fmt.Fprintf(stderr, "credential bootstrap: -actor-type must be 'Human' or 'Agent' (got %q)\n", *actorType)
		return 2
	}

	if *admin && t != actor.TypeHuman {
		fmt.Fprintln(stderr, "credential bootstrap: -admin is only valid with -actor-type Human")
		return 2
	}

	// People log in to the web UI with a username and password; agents
	// connect over MCP with a token (spec 2026-11-12 §1).
	if t == actor.TypeAgent {
		if *rawUsername != "" {
			fmt.Fprintln(stderr, "credential bootstrap: -username is only valid with -actor-type Human")
			return 2
		}
		actorID, token, err := authentication.Bootstrap(ctx, actors, credentials, t, name)
		if errors.Is(err, actor.ErrNameTaken) {
			fmt.Fprintf(stderr, "credential bootstrap: -name: %v\n", err)
			return 2
		}
		if err != nil {
			fmt.Fprintf(stderr, "credential bootstrap: %v\n", err)
			return 1
		}
		cliRecord(ctx, audits, audit.Event{Action: "actor.create", TargetType: "actor", TargetID: actorID,
			Detail: map[string]any{"type": string(t), "credential_id": credentialID(token), "expires_at": nil}})
		fmt.Fprintf(stdout, "Actor ID: %s\n", actorID)
		fmt.Fprintf(stdout, "Credential: %s\n", token)
		fmt.Fprintln(stdout, "This token is shown exactly once and cannot be retrieved again — store it now.")
		return 0
	}

	username, err := authentication.NormalizeUsername(*rawUsername)
	if err != nil {
		fmt.Fprintf(stderr, "credential bootstrap: -username is required for Human actors: %v\n", err)
		return 2
	}
	// Check before creating anything so a taken name never leaves a
	// half-created actor behind.
	switch _, err := logins.GetByUsername(ctx, username); {
	case err == nil:
		fmt.Fprintf(stderr, "credential bootstrap: -username: %v\n", authentication.ErrUsernameTaken)
		return 2
	case !errors.Is(err, authentication.ErrLoginNotFound):
		fmt.Fprintf(stderr, "credential bootstrap: %v\n", err)
		return 1
	}

	a := actor.New(actor.TypeHuman)
	a.Name = name
	if err := actors.Create(ctx, a); err != nil {
		if errors.Is(err, actor.ErrNameTaken) {
			fmt.Fprintf(stderr, "credential bootstrap: -name: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "credential bootstrap: %v\n", err)
		return 1
	}
	cliRecord(ctx, audits, audit.Event{Action: "actor.create", TargetType: "actor", TargetID: a.ID, Detail: map[string]any{"type": "Human", "username": username}})
	if *admin {
		grant := access.NewAssignment(a.ID, access.AdminRole, nil, a.ID)
		if err := acc.CreateAssignment(ctx, grant); err != nil {
			fmt.Fprintf(stderr, "credential bootstrap: actor %s was created without admin rights: %v\n", a.ID, err)
			return 1
		}
		cliRecord(ctx, audits, audit.Event{Action: "assignment.create", TargetType: "assignment", TargetID: grant.ID, Detail: map[string]any{"actor_id": a.ID, "role_id": access.AdminRole}})
	}
	pw, err := authentication.SetLogin(ctx, logins, actors, a.ID, username, "", true)
	if err != nil {
		fmt.Fprintf(stderr, "credential bootstrap: actor %s was created but has no password yet (%v); run: agentverdicts credential password -actor %s -username %s\n", a.ID, err, a.ID, username)
		return 1
	}
	fmt.Fprintf(stdout, "Actor ID: %s\n", a.ID)
	fmt.Fprintf(stdout, "Username: %s\n", username)
	fmt.Fprintf(stdout, "Password: %s\n", pw)
	fmt.Fprintln(stdout, "Temporary password — shown once. A new password is required at first login.")
	return 0
}

// runCredentialPassword sets a new temporary login password — the
// recovery path when nobody can log in to reset it from the web UI.
func runCredentialPassword(ctx context.Context, args []string, stdout, stderr io.Writer, actors actor.Repository, logins authentication.LoginRepository, audits audit.Repository) int {
	fs := flag.NewFlagSet("credential password", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rawUsername := fs.String("username", "", "login username (required)")
	actorID := fs.String("actor", "", "Human actor ID; needed only to give an actor its first username")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	username, err := authentication.NormalizeUsername(*rawUsername)
	if err != nil {
		fmt.Fprintf(stderr, "credential password: -username: %v\n", err)
		return 2
	}
	id := *actorID
	if id == "" {
		l, err := logins.GetByUsername(ctx, username)
		if err != nil {
			fmt.Fprintf(stderr, "credential password: no login %q — pass -actor <id> to create one\n", username)
			return 1
		}
		id = l.ActorID
	}
	pw, err := authentication.SetLogin(ctx, logins, actors, id, username, "", true)
	if err != nil {
		fmt.Fprintf(stderr, "credential password: %v\n", err)
		return 1
	}
	cliRecord(ctx, audits, audit.Event{Action: "actor.password_reset", TargetType: "actor", TargetID: id, Detail: map[string]any{"must_change": true}})
	fmt.Fprintf(stdout, "Username: %s\n", username)
	fmt.Fprintf(stdout, "Password: %s\n", pw)
	fmt.Fprintln(stdout, "Temporary password — shown once. A new password is required at first login.")
	return 0
}

// cliRecord logs an operator CLI event (no actor); a failed write is only
// logged (audit.Record).
func cliRecord(ctx context.Context, audits audit.Repository, e audit.Event) {
	if audits != nil {
		audit.Record(ctx, audits, e)
	}
}

func credentialID(token string) string { id, _, _ := strings.Cut(token, "."); return id }

func runCredentialIssue(ctx context.Context, args []string, stdout, stderr io.Writer, actors actor.Repository, credentials authentication.Repository, audits audit.Repository) int {
	fs := flag.NewFlagSet("credential issue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	actorID := fs.String("actor", "", "existing Actor ID to issue a new credential for (required)")
	label := fs.String("label", "", "note shown next to the token, up to 64 characters")
	days := fs.Int("expires-in-days", authentication.DefaultExpiryDays, "token lifetime: 30, 90 or 365 days, or 0 for a token that never expires")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *actorID == "" {
		fmt.Fprintln(stderr, "credential issue: -actor is required")
		return 2
	}
	if *days != 0 && *days != 30 && *days != 90 && *days != 365 {
		fmt.Fprintln(stderr, "credential issue: -expires-in-days must be 30, 90, 365 or 0 (never)")
		return 2
	}
	var expires *time.Time
	if *days != 0 {
		t := time.Now().AddDate(0, 0, *days)
		expires = &t
	}

	token, err := authentication.IssueWith(ctx, actors, credentials, *actorID, *label, expires)
	if err != nil {
		fmt.Fprintf(stderr, "credential issue: %v\n", err)
		return 1
	}
	lbl, _ := authentication.NormalizeLabel(*label)
	var exp *string
	if expires != nil {
		s := expires.UTC().Format(time.RFC3339Nano)
		exp = &s
	}
	cliRecord(ctx, audits, audit.Event{Action: "credential.issue", TargetType: "credential", TargetID: credentialID(token),
		Detail: map[string]any{"actor_id": *actorID, "label": lbl, "expires_at": exp}})
	fmt.Fprintf(stdout, "Credential: %s\n", token)
	fmt.Fprintln(stdout, "This credential is shown exactly once and cannot be retrieved again — store it now.")
	return 0
}

func runCredentialRevoke(ctx context.Context, args []string, stdout, stderr io.Writer, credentials authentication.Repository, audits audit.Repository) int {
	fs := flag.NewFlagSet("credential revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	credentialID := fs.String("credential", "", "credential_id to revoke — the non-secret identifier printed at issuance, NOT the secret/token itself (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *credentialID == "" {
		fmt.Fprintln(stderr, "credential revoke: -credential is required")
		return 2
	}

	c, err := credentials.Get(ctx, *credentialID)
	if err == nil {
		err = authentication.Revoke(ctx, credentials, *credentialID)
	}
	if err != nil {
		fmt.Fprintf(stderr, "credential revoke: %v\n", err)
		return 1
	}
	if c.RevokedAt == nil {
		cliRecord(ctx, audits, audit.Event{Action: "credential.revoke", TargetType: "credential", TargetID: c.ID, Detail: map[string]any{"actor_id": c.ActorID}})
	}
	fmt.Fprintf(stdout, "Credential %s revoked.\n", *credentialID)
	return 0
}
