export type QuoteStatus =
  | "REAL_TIME"
  | "DELAYED"
  | "INDICATIVE"
  | "SIMULATED"
  | "STALE"
  | "UNAVAILABLE";

export type Instrument = {
  id: string;
  symbol: string;
  display_name: string;
  asset_type: string;
  primary_exchange: string;
  currency: string;
  listed: boolean;
  capability: {
    searchable: boolean;
    quote_enabled: boolean;
    paper_tradable: boolean;
    live_tradable: boolean;
    fractional_enabled: boolean;
    transfer_out_enabled: boolean;
    rwa_mint_enabled: boolean;
    user_eligible: boolean;
    disabled_reason: string | null;
    policy_version: string;
  };
};

export type Quote = {
  instrument_id: string;
  symbol: string;
  replay_cursor: number;
  bid: string | null;
  ask: string | null;
  last: string | null;
  status: QuoteStatus;
  market_status: string;
  observed_at: string;
  provider: string;
};

export type Fill = {
  id: string;
  external_event_id: string;
  sequence: number;
  quantity: string;
  price: string;
  consideration: string;
  occurred_at: string;
};

export type Order = {
  id: string;
  symbol: string;
  side: "BUY" | "SELL";
  order_type: "MARKET" | "LIMIT";
  time_in_force: "DAY" | "GTC";
  quantity: string;
  limit_price: string | null;
  status: string;
  rejection_code: string | null;
  filled_quantity: string;
  average_fill_price: string | null;
  reference_price: string;
  quote_status: QuoteStatus;
  quote_observed_at: string;
  replay_cursor: number;
  provider_order_id: string;
  deterministic_replay: boolean;
  created_at: string;
  updated_at: string;
  fills: Fill[];
};

export type Position = {
  symbol: string;
  quantity: string;
  average_cost: string;
  cost_basis: string;
  realized_pnl: string;
  market_price: string;
  market_value: string;
  quote_status: QuoteStatus;
  updated_at: string;
};

export type Portfolio = {
  cash: {
    settled: string;
    withdrawable: string;
    provisional_buying_power: string;
    total_buying_power: string;
  };
  positions: Position[];
};

export type Registration = {
  account: {
    id: string;
    fixture_id: string;
    customer_reference: string;
    initial_cash: string;
    created_at: string;
  };
  access_token: string;
  mode: "SIMULATED";
  replayed: boolean;
};

export type SubmitOrder = {
  symbol: string;
  side: "BUY" | "SELL";
  order_type: "MARKET" | "LIMIT";
  time_in_force: "DAY" | "GTC";
  quantity: string;
  limit_price?: string | null;
};

export class APIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "APIError";
  }
}

const baseURL = (
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080"
).replace(/\/$/, "");

async function request<T extends object>(
  path: string,
  init: RequestInit = {},
  accessToken?: string,
  idempotencyKey?: string,
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body) headers.set("Content-Type", "application/json");
  if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`);
  if (idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);

  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    headers,
    cache: "no-store",
  });
  const payload = (await response.json()) as
    | T
    | { error?: { code?: string; message?: string } };
  if (!response.ok) {
    const error = "error" in payload ? payload.error : undefined;
    throw new APIError(
      error?.code ?? "UNEXPECTED_RESPONSE",
      error?.message ?? "The request could not be completed.",
      response.status,
    );
  }
  return payload as T;
}

export const paperAPI = {
  register(fixtureID: string): Promise<Registration> {
    return request("/v1/paper/registrations", {
      method: "POST",
      body: JSON.stringify({ fixture_id: fixtureID }),
    });
  },
  search(query: string): Promise<{ items: Instrument[] }> {
    return request(
      `/v1/instruments?q=${encodeURIComponent(query)}&page_size=20`,
    );
  },
  quote(symbol: string): Promise<Quote> {
    return request(`/v1/market-data/quotes/${encodeURIComponent(symbol)}`);
  },
  portfolio(accessToken: string): Promise<Portfolio> {
    return request("/v1/portfolio", {}, accessToken);
  },
  orders(accessToken: string): Promise<{ items: Order[] }> {
    return request("/v1/orders?page_size=50", {}, accessToken);
  },
  submit(
    accessToken: string,
    order: SubmitOrder,
    idempotencyKey: string,
  ): Promise<Order> {
    return request(
      "/v1/orders",
      { method: "POST", body: JSON.stringify(order) },
      accessToken,
      idempotencyKey,
    );
  },
  replay(
    accessToken: string,
    orderID: string,
    idempotencyKey: string,
  ): Promise<Order> {
    return request(
      `/v1/orders/${encodeURIComponent(orderID)}/replay`,
      { method: "POST" },
      accessToken,
      idempotencyKey,
    );
  },
  cancel(
    accessToken: string,
    orderID: string,
    idempotencyKey: string,
  ): Promise<Order> {
    return request(
      `/v1/orders/${encodeURIComponent(orderID)}/cancel`,
      { method: "POST" },
      accessToken,
      idempotencyKey,
    );
  },
};

export function canTrade(instrument: Instrument | null): boolean {
  return instrument?.capability.paper_tradable === true;
}

export function actionDisabledReason(
  instrument: Instrument | null,
  quantity: string,
  orderType: "MARKET" | "LIMIT" = "MARKET",
  limitPrice?: string | null,
): "NO_INSTRUMENT" | "VIEW_ONLY" | "NO_QUANTITY" | "NO_LIMIT_PRICE" | null {
  if (!instrument) return "NO_INSTRUMENT";
  if (!instrument.capability.paper_tradable) return "VIEW_ONLY";
  if (
    !/^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$/.test(quantity) ||
    /^0(?:\.0+)?$/.test(quantity)
  ) {
    return "NO_QUANTITY";
  }
  if (
    orderType === "LIMIT" &&
    (!limitPrice ||
      !/^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$/.test(limitPrice) ||
      /^0(?:\.0+)?$/.test(limitPrice))
  ) {
    return "NO_LIMIT_PRICE";
  }
  return null;
}
