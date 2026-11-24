import { describe, expect, it } from "vitest";
import { contributionHref, legacyHref, parseRoute, projectHref } from "./routes";

describe("routes", () => {
  it("builds readable paths, encoding any project id", () => {
    expect(projectHref("payments")).toBe("/p/payments/");
    expect(projectHref("a/b c")).toBe("/p/a%2Fb%20c/");
    expect(projectHref("p", new URLSearchParams({ tab: "open" }))).toBe("/p/p/?tab=open");
    expect(projectHref("p", new URLSearchParams())).toBe("/p/p/");
    expect(contributionHref("a/b", "123")).toBe("/p/a%2Fb/c/123/");
  });
  it("parses what it builds", () => {
    expect(parseRoute(projectHref("a/b c"))).toEqual({ project: "a/b c", id: null });
    expect(parseRoute(contributionHref("a/b", "x-1"))).toEqual({ project: "a/b", id: "x-1" });
    expect(parseRoute("/p/demo")).toEqual({ project: "demo", id: null });
  });
  it("rejects anything else", () => {
    for (const p of ["/p/abc/def", "/p/projc/foo/", "/p/", "/p/a/b/", "/p/a/c/", "/p/a/c/1/x/", "/x/a/", "/p/%E0%A4%A/"]) expect(parseRoute(p)).toBeNull();
  });
  it("maps the old query URLs", () => {
    expect(legacyHref("/project/", new URLSearchParams("id=a/b&tab=open"))).toBe("/p/a%2Fb/?tab=open");
    expect(legacyHref("/contribution/", new URLSearchParams("project=p&id=9"))).toBe("/p/p/c/9/");
    expect(legacyHref("/project/", new URLSearchParams())).toBe("/");
    expect(legacyHref("/contribution/", new URLSearchParams("project=p"))).toBe("/p/p/");
    expect(legacyHref("/contribution/", new URLSearchParams("project=p&id=9"), "#evidence")).toBe("/p/p/c/9/#evidence");
  });
});
