export type CardRecord = {
  id: string;
  card_type: "VIRTUAL" | "PLASTIC" | "METAL";
  status: string;
  display_name: string;
  last4: string;
  pin_set: boolean;
  apple_wallet_status: "UNAVAILABLE_SIMULATOR";
  google_wallet_status: "UNAVAILABLE_SIMULATOR";
  replayed: boolean;
  created_at: string;
  updated_at: string;
};

export type CardProfile = {
  customer_reference: string;
  repayment_mode: "CASH_ONLY" | "CASH_THEN_AUTO_SELL" | "MONTHLY_STATEMENT";
  spending_status: "ACTIVE" | "FROZEN";
  spending_status_reason: string | null;
  policy_version: string;
  cards: CardRecord[];
  mode: "SIMULATED";
};

export type CardCollateralDriver = {
  symbol: string;
  quantity: string;
  reference_price_usd: string;
  eligible_value_usd: string;
  quote_status: "SIMULATED" | "STALE" | "UNAVAILABLE";
  market_status: "OPEN" | "CLOSED" | "HALTED";
  eligible: boolean;
  exclusion_reason: string | null;
};

export type CardSpendingPower = {
  cash_eligible_usd: string;
  collateral_eligible_usd: string;
  gross_usd: string;
  outstanding_holds_usd: string;
  receivable_usd: string;
  available_usd: string;
  repayment_mode: CardProfile["repayment_mode"];
  spending_status: string;
  primary_explanation: string;
  drivers: CardCollateralDriver[];
  calculated_at: string;
  mode: "SIMULATED";
};

export type CardAuthorization = {
  id: string;
  card_id: string;
  merchant_name: string;
  merchant_amount: string;
  merchant_currency: string;
  authorized_usd: string;
  status: string;
  offline: boolean;
  entry_mode: string;
  decline_code: string | null;
  replayed: boolean;
  occurred_at: string;
};

export type CardAutoSellExecution = {
  id: string;
  symbol: string;
  protected_limit_price: string;
  execution_price: string;
  filled_quantity: string;
  proceeds_usd: string;
  status: string;
  failure_code: string | null;
};

export type CardCapture = {
  id: string;
  authorization_id: string;
  merchant_amount: string;
  merchant_currency: string;
  clearing_fx_rate: string;
  settled_usd: string;
  tip_usd: string;
  cash_repaid_usd: string;
  auto_sell_repaid_usd: string;
  refunded_usd: string;
  status: string;
  auto_sell_executions: CardAutoSellExecution[];
  replayed: boolean;
  occurred_at: string;
};

export type CardDispute = {
  id: string;
  capture_id: string;
  amount_usd: string;
  reason_code: string;
  status: string;
  outcome: string | null;
  compliance_case_id: string;
  opened_at: string;
  resolved_at: string | null;
};

export type CardStatement = {
  id: string;
  period_start: string;
  period_end: string;
  amount_due_usd: string;
  status: string;
  due_at: string;
  paid_at: string | null;
};

export type CardNotification = {
  id: string;
  event_type: string;
  title_key: string;
  body_key: string;
  delivery_status: string;
  created_at: string;
};

export class CardAPIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly nextAction: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "CardAPIError";
  }
}

const baseURL = (
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080"
).replace(/\/$/, "");

async function request<T extends object>(
  path: string,
  accessToken: string,
  init: RequestInit = {},
  idempotencyKey?: string,
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${accessToken}`);
  if (init.body) headers.set("Content-Type", "application/json");
  if (idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);
  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    headers,
    cache: "no-store",
  });
  const payload = (await response.json()) as
    | T
    | { error?: { code?: string; message?: string; next_action?: string } };
  if (!response.ok) {
    const error = "error" in payload ? payload.error : undefined;
    throw new CardAPIError(
      error?.code ?? "UNEXPECTED_RESPONSE",
      error?.message ?? "The Card request could not be completed.",
      error?.next_action ?? "Refresh Card activity and try again.",
      response.status,
    );
  }
  return payload as T;
}

export const cardAPI = {
  profile(token: string): Promise<CardProfile> {
    return request("/v1/card", token);
  },
  spendingPower(token: string): Promise<CardSpendingPower> {
    return request("/v1/card/spending-power", token);
  },
  authorizations(token: string): Promise<{ items: CardAuthorization[] }> {
    return request("/v1/card/authorizations?page_size=50", token);
  },
  captures(token: string): Promise<{ items: CardCapture[] }> {
    return request("/v1/card/captures?page_size=50", token);
  },
  disputes(token: string): Promise<{ items: CardDispute[] }> {
    return request("/v1/card/disputes?page_size=50", token);
  },
  statements(token: string): Promise<{ items: CardStatement[] }> {
    return request("/v1/card/statements", token);
  },
  notifications(token: string): Promise<{ items: CardNotification[] }> {
    return request("/v1/card/notifications?page_size=20", token);
  },
  createCard(
    token: string,
    cardType: string,
    key: string,
  ): Promise<CardRecord> {
    return request(
      "/v1/card/cards",
      token,
      { method: "POST", body: JSON.stringify({ card_type: cardType }) },
      key,
    );
  },
  actOnCard(
    token: string,
    cardID: string,
    action: string,
    key: string,
  ): Promise<CardRecord> {
    return request(
      `/v1/card/cards/${encodeURIComponent(cardID)}/actions`,
      token,
      {
        method: "POST",
        body: JSON.stringify({ action, reason_code: "USER_REQUEST" }),
      },
      key,
    );
  },
  configureRepayment(
    token: string,
    repaymentMode: string,
    key: string,
  ): Promise<CardProfile> {
    return request(
      "/v1/card/repayment-mode",
      token,
      {
        method: "POST",
        body: JSON.stringify({ repayment_mode: repaymentMode }),
      },
      key,
    );
  },
  configureMandate(token: string, key: string) {
    return request(
      "/v1/card/auto-sell-mandate",
      token,
      {
        method: "PUT",
        body: JSON.stringify({
          enabled: true,
          allow_fractional: true,
          daily_max_usd: "5000",
          valid_until: new Date(
            Date.now() + 30 * 24 * 60 * 60 * 1000,
          ).toISOString(),
          assets: [
            { priority: 1, symbol: "TLT", minimum_retain_quantity: "0" },
            { priority: 2, symbol: "VTI", minimum_retain_quantity: "0" },
          ],
        }),
      },
      key,
    );
  },
  seedCollateral(token: string, symbol: string, quantity: string, key: string) {
    return request(
      "/internal/v1/simulators/card/collateral",
      token,
      { method: "POST", body: JSON.stringify({ symbol, quantity }) },
      key,
    );
  },
  authorize(
    token: string,
    body: {
      external_event_id: string;
      card_id: string;
      merchant_name: string;
      merchant_category_code: string;
      merchant_amount: string;
      merchant_currency: string;
      entry_mode: string;
      offline: boolean;
      simulation_scenario: string;
    },
  ): Promise<CardAuthorization> {
    return request(
      "/internal/v1/simulators/card/terminal/authorizations",
      token,
      {
        method: "POST",
        body: JSON.stringify(body),
      },
    );
  },
  capture(
    token: string,
    body: {
      external_event_id: string;
      authorization_id: string;
      merchant_amount: string;
      merchant_currency: string;
      final: boolean;
      simulation_scenario: string;
    },
  ): Promise<CardCapture> {
    return request("/internal/v1/simulators/card/terminal/captures", token, {
      method: "POST",
      body: JSON.stringify(body),
    });
  },
  openDispute(
    token: string,
    captureID: string,
    amountUSD: string,
    key: string,
  ): Promise<CardDispute> {
    return request(
      "/v1/card/disputes",
      token,
      {
        method: "POST",
        body: JSON.stringify({
          capture_id: captureID,
          amount_usd: amountUSD,
          reason_code: "MERCHANDISE_NOT_RECEIVED",
        }),
      },
      key,
    );
  },
};

export function cardIdempotencyKey(scope: string): string {
  return `${scope}-${Date.now()}-${crypto.randomUUID()}`;
}
