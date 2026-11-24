Every claim has a trust state. It is worked out from the stances of its assessments and challenges; nobody sets it by hand.

```diagram
state-matrix
```

## The four states

- `state:Favorable` Only support, from as many people as the project asks for.
- `state:Unfavorable` Only opposition.
- `state:Conflict` Both support and opposition: someone should look again.
- `state:InsufficientEvidence` No assessment yet, only neutral ones, or not enough supporters.

## What counts

- Assessments and challenges weigh the same. Only the stance decides the state.
- Each person counts once, however many replies they send.
- An author supporting their own claim does not count.
- Replies that were withdrawn, replaced, hidden or sent by a disabled actor no longer count.

```diagram
counting-example
```

> [!NOTE]
> Each project sets a **minimum number of supporters** (1 by default). Until enough people support it, a claim stays `state:InsufficientEvidence`. A single opposition is enough for `state:Unfavorable` or `state:Conflict`.

## Where to read the state

- The badge next to a claim's title, and on each row of a list.
- The **Summary** panel on the claim's page: what the state means, the supporters bar, the count of each stance.
- The project's **Needs review** tab: `state:Conflict` claims first, then `state:InsufficientEvidence`.
