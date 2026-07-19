import { describe, expect, it } from "vitest";

import { copy, enUS } from "./en-US";

describe("en-US catalog", () => {
  it("contains non-empty copy for every key", () => {
    for (const value of Object.values(enUS)) {
      expect(value.trim().length).toBeGreaterThan(0);
    }
  });

  it("returns copy by semantic key", () => {
    expect(copy("appName")).toBe("Cytisus Paper");
  });
});
