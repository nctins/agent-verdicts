import { describe, expect, it } from "vitest";
import { moveRow, shortcutKey } from "./shortcuts";

const ev = (key: string, target: Element | null = document.body, mods: Partial<KeyboardEvent> = {}) =>
  ({ key, target, metaKey: false, ctrlKey: false, altKey: false, defaultPrevented: false, ...mods }) as KeyboardEvent;

describe("shortcutKey", () => {
  it("is the key outside of fields", () => {
    expect(shortcutKey(ev("j"))).toBe("j");
    expect(shortcutKey(ev("?"))).toBe("?");
  });
  it("is null while typing, with a modifier, for named keys, or inside a dialog", () => {
    expect(shortcutKey(ev("j", document.createElement("input")))).toBeNull();
    expect(shortcutKey(ev("j", document.createElement("textarea")))).toBeNull();
    expect(shortcutKey(ev("k", document.body, { metaKey: true }))).toBeNull();
    expect(shortcutKey(ev("Enter"))).toBeNull();
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    const btn = document.createElement("button");
    dialog.append(btn);
    expect(shortcutKey(ev("c", btn))).toBeNull();
  });
});

describe("moveRow", () => {
  it("walks the row links and stops at the ends", () => {
    document.body.innerHTML = `<a href="#1" data-row-link>1</a><a href="#2" data-row-link>2</a>`;
    const [a, b] = document.querySelectorAll("a");
    moveRow(1);
    expect(document.activeElement).toBe(a);
    moveRow(1);
    expect(document.activeElement).toBe(b);
    moveRow(1);
    expect(document.activeElement).toBe(b);
    moveRow(-1);
    expect(document.activeElement).toBe(a);
  });
});
