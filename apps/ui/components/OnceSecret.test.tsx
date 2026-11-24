import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import en from "@/messages/en.json";
import { OnceSecret } from "./OnceSecret";

afterEach(() => vi.unstubAllGlobals());

function setup(onDone = vi.fn()) {
  const qc = new QueryClient();
  const ui = (
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <OnceSecret label="Password" value="s3cret-value" onDone={onDone} />
      </NextIntlClientProvider>
    </QueryClientProvider>
  );
  return { qc, onDone, ...render(ui) };
}

describe("OnceSecret", () => {
  it("shows the value with the shown-once warning", () => {
    setup();
    expect((screen.getByLabelText("Password") as HTMLInputElement).value).toBe("s3cret-value");
    expect(screen.getByText(en.admin.once.warning)).toBeTruthy();
  });

  it("copies to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    setup();
    await userEvent.click(screen.getByRole("button", { name: en.admin.once.copy }));
    expect(writeText).toHaveBeenCalledWith("s3cret-value");
    expect(await screen.findByText(en.admin.once.copied)).toBeTruthy();
  });

  it("selects the text when copying fails", async () => {
    vi.stubGlobal("navigator", { clipboard: { writeText: vi.fn().mockRejectedValue(new Error("no")) } });
    setup();
    await userEvent.click(screen.getByRole("button", { name: en.admin.once.copy }));
    expect(await screen.findByText(en.admin.once.copy_failed)).toBeTruthy();
    const input = screen.getByLabelText("Password") as HTMLInputElement;
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe("s3cret-value".length);
  });

  it("calls onDone", async () => {
    const { onDone } = setup();
    await userEvent.click(screen.getByRole("button", { name: en.admin.once.done }));
    expect(onDone).toHaveBeenCalledOnce();
  });
});
