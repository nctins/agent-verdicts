import { describe, expect, it } from "vitest";
import { mcpSetup } from "./mcp";

describe("mcpSetup", () => {
  it("puts the project in the URL, config and command, with a token placeholder", () => {
    const s = mcpSetup("https://av.example", "team-a");
    expect(s.url).toBe("https://av.example/mcp/team-a");
    expect(JSON.parse(s.config).mcpServers.agentverdicts).toEqual({
      type: "http",
      url: s.url,
      headers: { Authorization: "Bearer <token>" },
    });
    expect(s.cli).toContain(`claude mcp add --transport http agentverdicts ${s.url}`);
    expect(s.cli).toContain("Bearer <token>");
  });
});
