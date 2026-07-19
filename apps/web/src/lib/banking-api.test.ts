import { afterEach, describe, expect, it, vi } from "vitest";

import { bankingAPI, BankingAPIError, positiveDecimal } from "./banking-api";

afterEach(() => vi.unstubAllGlobals());

describe("banking API", () => {
  it("preserves the safe reason and next action returned by the server", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json(
          {
            error: {
              code: "INSUFFICIENT_WITHDRAWABLE_CASH",
              message: "Only settled and withdrawable USD can be withdrawn.",
              next_action:
                "Lower the amount or wait for eligible funds to settle.",
            },
          },
          { status: 422 },
        ),
      ),
    );

    await expect(
      bankingAPI.requestWithdrawal(
        "synthetic-token",
        { amount: "250.00" },
        "withdrawal-test-0001",
      ),
    ).rejects.toEqual(
      new BankingAPIError(
        "INSUFFICIENT_WITHDRAWABLE_CASH",
        "Only settled and withdrawable USD can be withdrawn.",
        "Lower the amount or wait for eligible funds to settle.",
        422,
      ),
    );
  });
});

describe("banking amount validation", () => {
  it("accepts fixed-point decimals and rejects zero, negative, and float-like syntax", () => {
    expect(positiveDecimal("10.25")).toBe(true);
    expect(positiveDecimal("0.000000000000000001")).toBe(true);
    expect(positiveDecimal("0")).toBe(false);
    expect(positiveDecimal("-1")).toBe(false);
    expect(positiveDecimal("1e3")).toBe(false);
  });
});
