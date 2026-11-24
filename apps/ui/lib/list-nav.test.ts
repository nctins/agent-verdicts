import { afterEach, describe, expect, it, vi } from "vitest";
import { loadListNav, neighbours, saveListNav } from "./list-nav";

afterEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
});

describe("list nav", () => {
  it("round-trips and finds neighbours", () => {
    saveListNav({ url: "/p/demo/?tab=review", ids: ["a", "b", "c"] });
    const nav = loadListNav();
    expect(neighbours(nav, "b")).toEqual({ prev: "a", next: "c", url: "/p/demo/?tab=review" });
    expect(neighbours(nav, "a")).toEqual({ prev: undefined, next: "b", url: "/p/demo/?tab=review" });
    expect(neighbours(nav, "zzz")).toBeNull();
  });
  it("is empty when storage throws or holds junk", () => {
    sessionStorage.setItem("av.list-nav", "{bad");
    expect(loadListNav()).toBeNull();
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked"); });
    expect(() => saveListNav({ url: "/", ids: [] })).not.toThrow();
    expect(loadListNav()).toBeNull();
    expect(neighbours(null, "a")).toBeNull();
  });
});
