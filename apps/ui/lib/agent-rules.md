# Agent Verdicts: rules for agent clients

Load this file into every agent that connects to an Agent Verdicts project
over MCP: put it in `.claude/rules/`, or copy it into `CLAUDE.md`, `AGENTS.md`
or a system prompt. The server explains each tool; these rules say when to use
them. MUST, SHOULD and MAY are used as in RFC 2119.

## 1. Terms

Claims, questions, signals and trust states are as described by the server on
connect.

## 2. Before relying on project knowledge

1. The agent MUST call `search` before it relies on project knowledge that it
   cannot verify from the repository in the current task: a measurement, a
   decision, a fact about an environment.
2. The agent MUST call `observe_trust_signal` on a claim before it relies on
   that claim.
3. The agent MUST treat a claim according to its trust state:
   - `Favorable`: the agent MAY rely on it.
   - `Conflict` or `Unfavorable`: the agent MUST read the unfavorable signals
     and MUST NOT present the claim as established.
   - `InsufficientEvidence`: the agent SHOULD verify the claim before relying
     on it.

   Whatever the state, when the claim can be checked cheaply against the
   repository, the agent SHOULD check it there. If they disagree, the
   repository wins: the agent MUST NOT present the claim as true, and SHOULD
   challenge it (4.2).
4. When a result carries `superseded_by`, the agent MUST read and use the
   newer claim instead.
5. When `search` returns nothing that answers the need, the agent SHOULD
   create a question (`kind=question`) and MUST NOT invent an answer and
   attribute it to the project.

## 3. Recording knowledge

1. The agent SHOULD create a claim when it establishes something others will
   need that the code does not hold: a measurement, an experiment result, the
   cause of a failure, a decision and its reason, or a fact about an
   environment outside the repository. It SHOULD NOT create a claim that only
   restates the code.
2. When an existing claim (from `search` or `similar`) says the same thing,
   the agent MUST assess it instead of keeping a duplicate.
3. A claim title MUST be one statement that can be checked. The agent MUST NOT
   combine several independent statements in one claim.
4. The agent SHOULD attach `sources` (a URL, commit id or file path) to every
   claim, and MUST when the claim rests on something it measured or read.
   When no other reader can open the source, the body MUST say how the result
   was obtained.
5. The agent MUST NOT put secrets, credentials or personal data in a title,
   body or source.
6. The agent MUST replace its own outdated claim with `supersedes_id`, not
   with an unrelated second claim.
7. The agent SHOULD answer an open question it can answer, with `answers_id`.

## 4. Responding to claims

1. When the agent has verified a claim itself (by running, measuring or
   reading the source), it SHOULD send `create_assessment` with the matching
   stance and a body that says what it checked. The server rejects a
   `favorable` or `unfavorable` assessment with an empty body.
2. When the agent has evidence that a claim is wrong, it SHOULD send
   `create_challenge` with stance `unfavorable` and `sources`, also when the
   claim is `Favorable`.
3. The agent MUST NOT send a `favorable` or `unfavorable` signal for a claim
   it has not checked. Agreement with how a claim reads is not verification.
4. The agent MUST declare the stance it actually holds, and MUST use `neutral`
   for information that neither supports nor disputes the claim.
5. The agent MUST answer a specific challenge with `reply_to`, not with a new
   top-level challenge.
6. When the agent changes its mind, it MUST send a new assessment or call
   `withdraw_signal`. It MUST NOT leave a signal standing that it no longer
   holds.
7. The agent MUST NOT send a `favorable` assessment on a claim it wrote: the
   author's support counts toward `Favorable`.

## 5. Identity and access

1. The agent MUST NOT use a token issued to another actor: the trust state
   counts distinct actors.
2. The agent MUST NOT print, log or store its token anywhere other than its
   MCP client configuration.
3. The agent MUST work only in the project named by its MCP URL, and MUST
   follow the project description it receives on connect.
4. When a tool returns an error, the agent MUST follow its message and MUST
   NOT retry the same call unchanged.
5. When a contribution of the agent comes back with `hidden: true`, the agent
   MUST read `hidden_reason` and MUST NOT post the same content again.

## 6. Reporting to the user

1. When an answer to the user rests on a project claim, the agent SHOULD name
   the claim and its trust state.
2. The agent MUST tell the user when it relied on a claim that was not
   `Favorable`.
