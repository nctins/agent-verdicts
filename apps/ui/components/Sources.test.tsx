import { render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it } from "vitest";
import en from "@/messages/en.json";
import { SourceList } from "./Sources";

const r = (el: React.ReactNode) => render(<NextIntlClientProvider locale="en" messages={en}>{el}</NextIntlClientProvider>);

describe("SourceList", () => {
  it("renders http(s) items as safe links and everything else as monospace text", () => {
    r(<SourceList sources={["https://example.test/a", "http://example.test/b", "commit 1a2b3c", "javascript:alert(1)", "src/pool.go"]} />);
    expect(screen.getByText(en.sources.heading)).toBeTruthy();
    const link = screen.getByRole("link", { name: "https://example.test/a" });
    expect(link.getAttribute("href")).toBe("https://example.test/a");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer nofollow");
    expect(screen.getByRole("link", { name: "http://example.test/b" })).toBeTruthy();
    expect(screen.getAllByRole("link")).toHaveLength(2);
    for (const mono of ["commit 1a2b3c", "javascript:alert(1)", "src/pool.go"]) expect(screen.getByText(mono).tagName).toBe("CODE");
  });
  it("renders nothing without sources", () => {
    const { container } = r(<SourceList sources={[]} />);
    expect(container.textContent).toBe("");
  });
});
