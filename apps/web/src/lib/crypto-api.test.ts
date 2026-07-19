import { describe, expect, it } from "vitest";

import { conversionDisabledReason, positiveDecimal } from "./crypto-api";

describe("crypto decimal validation", () => {
  it("accepts exact positive fractional quantities", () => {
    expect(positiveDecimal("0.00000001")).toBe(true);
    expect(positiveDecimal("25000")).toBe(true);
  });

  it("rejects floats represented by unsafe or invalid text", () => {
    expect(positiveDecimal("0")).toBe(false);
    expect(positiveDecimal("1e-8")).toBe(false);
    expect(positiveDecimal("1.1234567890123456789")).toBe(false);
  });
});

describe("crypto conversion controls", () => {
  it("requires distinct assets and a positive decimal", () => {
    expect(conversionDisabledReason("BTC", "BTC", "1")).toBe("SAME_ASSET");
    expect(conversionDisabledReason("USD", "BTC", "0")).toBe("INVALID_AMOUNT");
    expect(conversionDisabledReason("BTC", "ETH", "0.5")).toBeNull();
  });
});
