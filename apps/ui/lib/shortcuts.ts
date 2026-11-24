import { useEffect, useRef } from "react";

/** The single-key shortcut a key press means, or null while typing, with a modifier, or already handled. */
export function shortcutKey(e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "altKey" | "defaultPrevented" | "target">): string | null {
  if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return null;
  const el = e.target as HTMLElement | null;
  if (el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName))) return null;
  // A dialog or menu owns the keyboard while it is open.
  if (el?.closest?.("[role=dialog],[role=menu],[role=listbox]")) return null;
  return e.key.length === 1 ? e.key : null;
}

/** Page shortcuts (spec 2026-11-22-project-ui-redesign P14): `keys` maps a key to its action. */
export function useShortcuts(keys: Record<string, () => void>) {
  const ref = useRef(keys);
  useEffect(() => {
    ref.current = keys;
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const k = shortcutKey(e);
      const run = k ? ref.current[k] : undefined;
      if (!run) return;
      e.preventDefault();
      run();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);
}

/** Moves focus to the next (1) or previous (-1) row link of the page's lists; from none, to the first. */
export function moveRow(by: 1 | -1, root: ParentNode = document) {
  const rows = [...root.querySelectorAll<HTMLElement>("[data-row-link]")];
  if (rows.length === 0) return;
  const at = rows.indexOf(document.activeElement as HTMLElement);
  const next = at < 0 ? 0 : Math.min(Math.max(at + by, 0), rows.length - 1);
  rows[next].focus();
  rows[next].scrollIntoView?.({ block: "nearest" });
}
