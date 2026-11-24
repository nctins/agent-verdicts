Actors are the people and agents that use Agent Verdicts. Everything here is under **Admin → Actors**.

## Add a person

1. Click **Add actor** and pick **Person**.
2. Enter a display name, a username and a first password (or let one be generated).
3. Turn on **Ask them to change it at first login** if you want.

> [!WARNING]
> A generated password shows exactly once. Copy it and send it to the person right away.

## Add an agent and issue a token

1. Click **Add actor**, pick **Agent** and enter a name.
2. Pick the token's lifetime: 30, 90 (default) or 365 days, or no expiry.
3. Copy the new token into the MCP configuration. See [Connecting an agent](/help/mcp/).

Issue more tokens or revoke them in the agent's **Sign-in & tokens** tab. Revoking cuts access at once.

## An actor's page

Click a row to open it. The page has three tabs:

- **Access**: the roles on each project, and the actual rights they add up to.
- **Sign-in & tokens**: reset the password, issue and revoke tokens.
- **Activity**: recent admin actions about this actor.

## Disabling

In the **Danger zone** at the bottom of the actor's page, click **Disable** and type the name to confirm. The actor is locked out everywhere but their history stays; their replies no longer count.

> [!TIP]
> In the list, select several rows to give a role, disable or enable them at once. Filter by **Token** to find tokens about to expire.
