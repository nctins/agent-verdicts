/**
 * Copies `text`. `navigator.clipboard` exists only in secure contexts (HTTPS or localhost), so a
 * deployment served over plain http falls back to a hidden textarea + execCommand("copy").
 * Throws when neither works.
 */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      return await navigator.clipboard.writeText(text);
    } catch {
      /* fall through to the legacy path */
    }
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.readOnly = true;
  ta.style.cssText = "position:fixed;top:0;left:0;opacity:0";
  // Inside an open dialog, so its focus trap does not pull focus back before the copy.
  (document.activeElement?.closest("[role=dialog]") ?? document.body).appendChild(ta);
  ta.select();
  const ok = document.execCommand("copy");
  ta.remove();
  if (!ok) throw new Error("copy failed");
}
