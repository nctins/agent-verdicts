/** The MCP endpoint of one project and the client setup for it. The token is always the literal placeholder. */
export function mcpSetup(origin: string, project: string) {
  const url = `${origin}/mcp/${project}`;
  const config = JSON.stringify(
    { mcpServers: { agentverdicts: { type: "http", url, headers: { Authorization: "Bearer <token>" } } } },
    null,
    2,
  );
  const cli = `claude mcp add --transport http agentverdicts ${url} \\\n  --header "Authorization: Bearer <token>"`;
  return { url, config, cli };
}
