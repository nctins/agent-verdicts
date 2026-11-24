import type { paths } from "./api/schema";

type CreateActor = paths["/admin/actors"]["post"]["requestBody"]["content"]["application/json"];

type Create = { type: "Human" | "Agent"; name: string; username: string; password: string; mustChange: boolean | null };

// A generated password (empty field) forces a change at first login unless the admin says otherwise.
const mustChange = (choice: boolean | null, password: string) => choice ?? password === "";

export function actorBody(f: Create): CreateActor {
  const name = f.name.trim();
  if (f.type === "Agent") return { type: "Agent", name };
  return {
    type: "Human",
    name,
    username: f.username.trim(),
    ...(f.password ? { password: f.password } : {}),
    must_change: mustChange(f.mustChange, f.password),
  };
}

export function resetBody(f: { username: string; password: string; mustChange: boolean | null }) {
  const username = f.username.trim();
  return {
    ...(username ? { username } : {}),
    ...(f.password ? { password: f.password } : {}),
    must_change: mustChange(f.mustChange, f.password),
  };
}

// Same rule as the API (project.ValidateID); the server still decides.
const SLUG = /^[a-z0-9][a-z0-9_-]{0,63}$/;
export const projectIdError = (raw: string): "required" | "invalid" | null => {
  const id = raw.trim();
  return id === "" ? "required" : SLUG.test(id) ? null : "invalid";
};

export const EXPIRY_CHOICES = ["30", "90", "365", "never"] as const;
export type ExpiryChoice = (typeof EXPIRY_CHOICES)[number];

/** Body of the issue-token request; the expiry is always explicit ("never" is null). */
export const issueBody = (label: string, expiry: ExpiryChoice) => ({
  ...(label.trim() && { label: label.trim() }),
  expires_in_days: expiry === "never" ? null : (Number(expiry) as 30 | 90 | 365),
});

const WEEK = 7 * 24 * 3600 * 1000;
/** How a token's expiry reads at `now`: never, ok, soon (within 7 days) or expired. */
export function expiryState(expiresAt: string | null, now: number): "never" | "ok" | "soon" | "expired" {
  if (!expiresAt) return "never";
  const left = new Date(expiresAt).getTime() - now;
  return left <= 0 ? "expired" : left < WEEK ? "soon" : "ok";
}
