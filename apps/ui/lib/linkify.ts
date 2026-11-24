// Only http(s): anything else (javascript:, data:, …) stays plain text.
const URL_RE = /https?:\/\/[^\s<>"]+/g;
const TRAILING = /[.,;:!?)]+$/;

export function linkify(text: string): Array<{ text: string; href?: string }> {
  const out: Array<{ text: string; href?: string }> = [];
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    const url = m[0].replace(TRAILING, "");
    if (!/^https?:\/\/./.test(url)) continue;
    if (m.index > last) out.push({ text: text.slice(last, m.index) });
    out.push({ text: url, href: url });
    last = m.index + url.length;
  }
  if (last < text.length || out.length === 0) out.push({ text: text.slice(last) });
  return out;
}
