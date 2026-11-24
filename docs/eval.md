# Evaluating whether it helps

[Tiếng Việt](vi/eval.md)

How to find out whether Agent Verdicts actually helps your team and agents,
rather than only being used.

Usage numbers are not enough. An agent can call `search` ten times, get a
wrong claim back, and answer wrongly. To know whether the system helps you
have to compare it with not having it, on the same work, with answers you can
check.

## What "helps" means

Agent Verdicts helps if agents get at least one of the first two without the
third getting worse:

| Axis | Meaning | Measure |
|---|---|---|
| More correct | More right answers, especially when the knowledge lives only with other people or agents | Share of answers matching the known answer |
| Cheaper | The same accuracy with fewer tokens, tool calls or minutes | Tokens and cost reported by the agent client, tool calls, time |
| Not misled | A wrong or outdated claim is not repeated | Share of answers that state a planted wrong value |

Also check that agents do not make things up: for a question nobody knows the
answer to, the right answer is "unknown".

## 1. Run a small comparison

1. **Write 20–30 questions about your project with known answers.** Mix:
   - questions the code answers (a baseline: the system should not slow these
     down);
   - questions whose answer is only known to people (a measurement, a
     decision, an environment detail);
   - questions where the project holds a wrong or outdated claim next to the
     right one;
   - a few questions nobody can answer.
2. **Put the knowledge into a test project**: the right claims, the wrong or
   outdated ones, and the assessments and challenges on them, as your team
   would write them.
3. **Run the same agent, model and settings in separate groups**, giving
   each only the repository plus one knowledge source:
   - **A**: no extra knowledge (baseline);
   - **B**: Agent Verdicts over MCP, with
     [the agent rules](instructions/agent-verdicts.md) loaded;
   - **C** (optional): the same project exported as one plain notes file,
     including who agreed and disagreed. If B is no better than C, a shared
     file may be enough for you.
4. **Fix the grading rules before running**, ask for a short final answer
   (or `UNKNOWN`), and run each question at least three times per group.
5. **Compare** correctness, misled answers, `UNKNOWN` on unanswerable
   questions, and cost. With a few dozen questions, report counts next to
   percentages: small differences are noise.

Keep the answer key and your notes where the agent under test cannot read
them.

Limits to keep in mind: short-answer questions are easier than real work,
the test data is written by you, and long-term value (knowledge piling up
over months) does not show up in one run.

## 2. Watch a running project

Once agents use a real project, the product shows whether its knowledge is
being used and kept healthy. These describe activity; they do not prove that
agents answer better.

| Question | Where to look |
|---|---|
| Is the knowledge read? | Project **Overview**: agent reads over 30 days and contributions never read (`member:Manage`; API `GET /api/v1/projects/{p}/reads`). Only MCP reads are counted, never an author's own. |
| What needs a person? | The **Needs review** tab (claims in `Conflict` or `InsufficientEvidence`) and the admin overview (conflicts changed in the last 7 days, questions open for over 14 days). |
| Do an agent's claims hold up? | The **Claims** tab filtered by that actor and by state: how many are `Favorable`, disputed, or superseded. |
| Is support real? | Open `Favorable` claims and read the assessments. A claim supported once with an empty reason looks checked but may not be; raise the project's `min_favorable` if that is common. |

The server cannot tell you whether agents search before writing, what they
spend in tokens, or whether their answers to users were right. Only a
comparison like the one above answers that.

## 3. Search quality

`apps/api/eval` measures retrieval on a fixed dataset (266 contributions, 108
queries, Vietnamese and English). It loads the dataset into a temporary
schema, runs each available pipeline, and prints R@1, R@5, MRR and latency.

```sh
docker compose up -d db
docker compose run --rm api go run ./eval                       # BM25 only
docker compose run --rm api go run ./eval \
  -embed-url <url> -embed-model <model> \
  -rerank-url <url> -rerank-model <model>                       # + vector, + rerank
docker compose run --rm api go run ./eval -filler 10000         # latency with 10k extra contributions
```

Use the same provider URLs and models as in **Admin → Search settings**.
The dataset was written with an AI assistant and is biased toward how it
phrases things: use the results to compare pipelines, not as absolute
numbers. To test on your own content, add contributions and queries to
`apps/api/eval/dataset.json`.
