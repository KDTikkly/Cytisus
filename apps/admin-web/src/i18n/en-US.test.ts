import { describe, expect, it } from "vitest";

import { enUS } from "./en-US";

describe("admin en-US catalog", () => {
  it("contains non-empty copy for every key", () => {
    for (const value of Object.values(enUS)) {
      expect(value.trim().length).toBeGreaterThan(0);
    }
  });

  it("keeps high-risk actions unavailable", () => {
    expect(enUS.actionsValue).toBe("Unavailable");
  });
});
