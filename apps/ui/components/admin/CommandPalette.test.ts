import { expect, it } from "vitest";
import { matchItems } from "./CommandPalette";

const item = (group: string, label: string, hint?: string) => ({ id: group + label, group, label, hint, href: "/" });

it("matches label or hint without accents, at most six per group", () => {
  const items = [item("a", "Bình", "@binh"), item("a", "Lan"), ...Array.from({ length: 9 }, (_, i) => item("b", `p${i}`))];
  expect(matchItems(items, "binh").map((x) => x.label)).toEqual(["Bình"]);
  expect(matchItems(items, "@bi").map((x) => x.label)).toEqual(["Bình"]);
  expect(matchItems(items, "").filter((x) => x.group === "b")).toHaveLength(6);
});
