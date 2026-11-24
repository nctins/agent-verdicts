import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Markdown } from "./Markdown";

const html = (text: string) => render(<Markdown text={text} />).container;

describe("Markdown", () => {
  it("renders each allowed element", () => {
    const c = html("para **bold** *it* `code`\n\n- a\n- b\n\n1. x\n2. y\n\n> quote\n\n```\nfenced\n```\n\n[l](https://a.b)");
    for (const sel of ["p", "strong", "em", "ul li", "ol li", "blockquote", "pre code", "a"]) expect(c.querySelector(sel), sel).not.toBeNull();
    expect([...c.querySelectorAll("code")].map((x) => x.textContent)).toEqual(["code", "fenced\n"]);
  });
  it("keeps single line breaks", () => {
    expect(html("a\nb").querySelector("br")).not.toBeNull();
  });
  it("shows HTML as text, never as elements", () => {
    const c = html("<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>");
    expect(c.querySelector("script")).toBeNull();
    expect(c.querySelector("img")).toBeNull();
    expect(c.textContent).toContain("<script>alert(1)</script>");
  });
  it("renders images as text", () => {
    const c = html("![alt text](http://x/y.png)");
    expect(c.querySelector("img")).toBeNull();
    expect(c.textContent).toContain("alt text");
  });
  it("links only http(s), with target and the full rel", () => {
    const c = html("[ok](https://a.b/c) [bad](javascript:alert(1)) [rel](/x)");
    const links = c.querySelectorAll("a");
    expect(links).toHaveLength(1);
    expect(links[0].getAttribute("rel")).toBe("noopener noreferrer nofollow");
    expect(links[0].getAttribute("target")).toBe("_blank");
    expect(c.textContent).toContain("bad");
  });
  it("links a bare URL without its trailing punctuation, but not inside code", () => {
    const c = html("go https://a.b/c. and `https://no.link`");
    const links = c.querySelectorAll("a");
    expect(links).toHaveLength(1);
    expect(links[0].getAttribute("href")).toBe("https://a.b/c");
    expect(c.textContent).toContain("c. and");
  });
  it("renders no heading above h3", () => {
    const c = html("# a\n\n## b\n\n### c\n\n#### d");
    expect(c.querySelector("h1, h2")).toBeNull();
    expect([...c.querySelectorAll("h3, h4")].map((h) => h.tagName)).toEqual(["H3", "H3", "H3", "H4"]);
  });
});
