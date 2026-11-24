import { render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it } from "vitest";
import en from "@/messages/en.json";
import { StateBadge } from "./StateBadge";

const r = (el: React.ReactNode) => render(<NextIntlClientProvider locale="en" messages={en}>{el}</NextIntlClientProvider>);

describe("StateBadge", () => {
  it.each(["Favorable", "Unfavorable", "Conflict", "InsufficientEvidence"] as const)("labels %s in words", (s) => {
    r(<StateBadge state={s} />);
    expect(screen.getByText(en.state[s])).toBeTruthy();
  });
  it("prefers the question status for questions", () => {
    r(<StateBadge state={null} questionStatus="answered" />);
    expect(screen.getByText(en.question_status.answered)).toBeTruthy();
  });
  it("renders nothing without a state", () => {
    const { container } = r(<StateBadge state={null} />);
    expect(container.textContent).toBe("");
  });
});
