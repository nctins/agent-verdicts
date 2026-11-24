import { BotIcon } from "lucide-react";
import { cn } from "@/lib/utils";

// A stable hue per seed (the actor's id when known), so the same author always looks the same
// and two people who share an initial still differ.
export const hue = (s: string) => [...s].reduce((h, c) => (h * 31 + c.codePointAt(0)!) % 360, 7);

// The first letter or digit of each of the first two words that have one: "Scout (agent)" is SA, not S(.
export const initials = (name: string) =>
  name.split(/\s+/).flatMap((w) => w.match(/[\p{L}\p{N}]/u) ?? []).slice(0, 2).join("").toUpperCase() || "?";

/**
 * Who wrote something, at a glance (spec 2026-11-23-ui-polish G3): a person is a circle with
 * initials, an agent a rounded square with a bot. Decorative: the name is always written next to it.
 */
export function Avatar({ name, type, seed, className }: { name: string; type?: string; seed?: string; className?: string }) {
  const agent = type === "Agent";
  return (
    <span
      aria-hidden
      data-actor-type={agent ? "agent" : "human"}
      className={cn(
        "inline-flex size-8 shrink-0 items-center justify-center text-xs font-semibold text-white",
        agent ? "rounded-lg" : "rounded-full",
        className,
      )}
      style={{ backgroundColor: `oklch(${agent ? "0.5 0.09" : "0.6 0.12"} ${hue(seed || name)})` }}
    >
      {agent ? <BotIcon className="size-[55%]" /> : initials(name)}
    </span>
  );
}
