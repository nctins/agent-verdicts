import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { beforeEach, describe, expect, it, vi } from "vitest";
import en from "@/messages/en.json";
import { ApiError } from "@/lib/api/errors";
import { SearchBox } from "./SearchBox";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

let state = { results: [] as unknown[], degraded: false, loading: false, settledText: "", stale: false, error: null as unknown };
vi.mock("@/lib/use-debounced-search", () => ({ useDebouncedSearch: () => state }));

const hit = { id: "c1", kind: "claim", title: "Retry webhooks", snippet: "use backoff", state: "Favorable" };

function setup() {
  const onSubmit = vi.fn();
  const onAsk = vi.fn();
  render(
    <NextIntlClientProvider locale="en" messages={en}>
      <SearchBox project="demo" onSubmit={onSubmit} onAsk={onAsk} />
    </NextIntlClientProvider>,
  );
  return { onSubmit, onAsk, box: screen.getByRole("combobox") };
}

beforeEach(() => {
  push.mockClear();
  state = { results: [], degraded: false, loading: false, settledText: "", stale: false, error: null };
});

describe("SearchBox", () => {
  it("offers to ask the question when nothing matches", async () => {
    state = { ...state, settledText: "webhook retry" };
    const { onAsk, box } = setup();
    await userEvent.type(box, "webhook retry");
    await userEvent.click(screen.getByRole("option", { name: en.search.ask_this }));
    expect(onAsk).toHaveBeenCalledWith("webhook retry");
  });

  it("does not offer to ask while the typed text is not settled yet", async () => {
    state = { ...state, settledText: "webhook", loading: true };
    const { box } = setup();
    await userEvent.type(box, "webhook retry");
    expect(screen.queryByRole("option", { name: en.search.ask_this })).toBeNull();
  });

  it("Enter during a pending search submits instead of opening a stale row", async () => {
    state = { ...state, results: [hit], settledText: "ret", loading: true };
    const { onSubmit, box } = setup();
    await userEvent.type(box, "retry{ArrowDown}{Enter}");
    expect(push).not.toHaveBeenCalled();
    expect(onSubmit).toHaveBeenCalledWith("retry");
  });

  it("shows a search error instead of nothing", async () => {
    state = { ...state, settledText: "", error: new ApiError(500, "internal") };
    const { box } = setup();
    await userEvent.type(box, "retry");
    expect(screen.getByText(en.errors.internal)).toBeTruthy();
  });

  it("Enter with nothing selected submits the text", async () => {
    state = { ...state, results: [hit], settledText: "retry" };
    const { onSubmit, box } = setup();
    await userEvent.type(box, "retry{Enter}");
    expect(onSubmit).toHaveBeenCalledWith("retry");
    expect(push).not.toHaveBeenCalled();
  });

  it("arrow + Enter opens the selected result", async () => {
    state = { ...state, results: [hit], settledText: "retry" };
    const { onSubmit, box } = setup();
    await userEvent.type(box, "retry{ArrowDown}{Enter}");
    expect(push).toHaveBeenCalledWith("/p/demo/c/c1/");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("Escape closes the list", async () => {
    state = { ...state, results: [hit], settledText: "retry" };
    const { box } = setup();
    await userEvent.type(box, "retry");
    expect(screen.queryByRole("listbox")).toBeTruthy();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("says when search is degraded", async () => {
    state = { ...state, results: [hit], settledText: "retry", degraded: true };
    const { box } = setup();
    await userEvent.type(box, "retry");
    expect(screen.getByText(en.search.degraded)).toBeTruthy();
  });
});
