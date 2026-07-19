import { afterEach, describe, expect, it, vi } from "vitest";

import {
  actionDisabledReason,
  APIError,
  Instrument,
  paperAPI,
} from "./paper-api";

const tradable: Instrument = {
  id: "8f3c34b4-264e-4ccf-92fb-074b0a4b6981",
  symbol: "AAPL",
  display_name: "Apple Inc. (synthetic fixture)",
  asset_type: "COMMON_STOCK",
  primary_exchange: "NASDAQ",
  currency: "USD",
  listed: true,
  capability: {
    searchable: true,
    quote_enabled: true,
    paper_tradable: true,
    live_tradable: false,
    fractional_enabled: true,
    transfer_out_enabled: false,
    rwa_mint_enabled: false,
    user_eligible: true,
    disabled_reason: null,
    policy_version: "paper-instrument-v1",
  },
};

afterEach(() => vi.unstubAllGlobals());

describe("paper order disabled reasons", () => {
  it("explains missing, view-only, quantity, and limit-price states", () => {
    expect(actionDisabledReason(null, "1")).toBe("NO_INSTRUMENT");
    expect(
      actionDisabledReason(
        {
          ...tradable,
          capability: {
            ...tradable.capability,
            paper_tradable: false,
            disabled_reason: "ASSET_TYPE_VIEW_ONLY",
          },
        },
        "1",
      ),
    ).toBe("VIEW_ONLY");
    expect(actionDisabledReason(tradable, "0")).toBe("NO_QUANTITY");
    expect(actionDisabledReason(tradable, "0.25")).toBeNull();
    expect(actionDisabledReason(tradable, "1", "LIMIT", "")).toBe(
      "NO_LIMIT_PRICE",
    );
  });
});

describe("paper API errors", () => {
  it("preserves stable server error codes for UI error states", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: {
                code: "INSTRUMENT_VIEW_ONLY",
                message: "This instrument can be viewed but is not eligible.",
              },
            }),
            { status: 422, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    await expect(
      paperAPI.submit(
        "synthetic-token",
        {
          symbol: "FIXADR",
          side: "BUY",
          order_type: "MARKET",
          time_in_force: "DAY",
          quantity: "1",
        },
        "web-order-0001",
      ),
    ).rejects.toEqual(
      new APIError(
        "INSTRUMENT_VIEW_ONLY",
        "This instrument can be viewed but is not eligible.",
        422,
      ),
    );
  });
});
