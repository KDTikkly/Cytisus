export type CryptoSymbol =
  | "BTC"
  | "ETH"
  | "SOL"
  | "XRP"
  | "BNB"
  | "DOGE"
  | "ADA"
  | "AVAX"
  | "LINK"
  | "LTC"
  | "USDC"
  | "USDT";

export type CryptoAsset = {
  symbol: CryptoSymbol;
  display_name: string;
  is_stablecoin: boolean;
  tradable: true;
  precision: number;
  quote_currency: "USD";
  networks: Array<{
    code: string;
    display_name: string;
    deposit_enabled: boolean;
    withdrawal_enabled: boolean;
    native_deployment: true;
  }>;
};

export type CryptoBalance = {
  asset: CryptoSymbol;
  settled: string;
  held: string;
  frozen: string;
  custody_model: "CUSTODIAL";
};

export type CryptoFill = {
  id: string;
  venue: "VENUE_A" | "VENUE_B" | "VENUE_C";
  external_fill_id: string;
  quantity: string;
  price: string;
  gross_usd: string;
  venue_fee_usd: string;
  occurred_at: string;
};

export type CryptoChildOrder = {
  id: string;
  sequence: number;
  venue: "VENUE_A" | "VENUE_B" | "VENUE_C";
  client_order_id: string;
  provider_order_id: string | null;
  requested_quantity: string;
  filled_quantity: string;
  quote_bid: string;
  quote_ask: string;
  venue_fee_rate: string;
  effective_unit_price: string;
  execution_price: string;
  gross_usd: string;
  venue_fee_usd: string;
  status: string;
  failure_code: string | null;
  quote_observed_at: string;
  fills: CryptoFill[];
};

export type CryptoLeg = {
  id: string;
  sequence: 1 | 2;
  side: "BUY" | "SELL";
  asset: CryptoSymbol;
  quote_currency: "USD";
  input_amount: string;
  filled_quantity: string;
  reference_price: string;
  average_execution_price: string;
  gross_usd: string;
  venue_fee_usd: string;
  platform_fee_usd: string;
  final_customer_usd: string;
  price_improvement_usd: string;
  status: string;
  failure_code: string | null;
  ledger_transaction_id: string | null;
  children: CryptoChildOrder[];
};

export type CryptoConversion = {
  id: string;
  source_asset: "USD" | CryptoSymbol;
  destination_asset: "USD" | CryptoSymbol;
  source_amount: string;
  simulation_scenario: string;
  status: string;
  reason_code: string | null;
  next_action: string;
  policy_version: string;
  compliance_case_id: string | null;
  legs: CryptoLeg[];
  replayed: boolean;
  mode: "SIMULATED";
  created_at: string;
  updated_at: string;
};

export type CryptoDepositAddress = {
  id: string;
  asset: CryptoSymbol;
  network: string;
  provider: string;
  external_address: string;
  memo: string | null;
  status: "ACTIVE" | "SUSPENDED";
  created_at: string;
  replayed: boolean;
  mode: "SIMULATED";
};

export type CryptoDeposit = {
  id: string;
  deposit_address_id: string;
  asset: CryptoSymbol;
  network: string;
  quantity: string;
  status: "CONFIRMED" | "REJECTED";
  provider_transaction_id: string;
  ledger_transaction_id: string | null;
  reason_code: string | null;
  created_at: string;
  replayed: boolean;
  mode: "SIMULATED";
};

export type CryptoWithdrawalAddress = {
  id: string;
  asset: CryptoSymbol;
  network: string;
  external_address: string;
  label: string;
  status: "COOLING_OFF" | "ACTIVE" | "SUSPENDED" | "REVOKED";
  risk_level: "LOW" | "ELEVATED" | "BLOCKED";
  cooling_until: string | null;
  cooling_reason: string;
  policy_version: string;
  created_at: string;
  updated_at: string;
  replayed: boolean;
  mode: "SIMULATED";
};

export type CryptoWithdrawal = {
  id: string;
  withdrawal_address_id: string;
  asset: CryptoSymbol;
  network: string;
  quantity: string;
  status: string;
  reason_code: string;
  next_action: string;
  provider_withdrawal_id: string;
  created_at: string;
  updated_at: string;
  replayed: boolean;
  mode: "SIMULATED";
};

export class CryptoAPIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly nextAction: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "CryptoAPIError";
  }
}

const baseURL = (
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080"
).replace(/\/$/, "");

async function request<T extends object>(
  path: string,
  accessToken?: string,
  init: RequestInit = {},
  idempotencyKey?: string,
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`);
  if (init.body) headers.set("Content-Type", "application/json");
  if (idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);
  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    headers,
    cache: "no-store",
  });
  const payload = (await response.json()) as
    | T
    | {
        error?: {
          code?: string;
          message?: string;
          next_action?: string;
        };
      };
  if (!response.ok) {
    const error = "error" in payload ? payload.error : undefined;
    throw new CryptoAPIError(
      error?.code ?? "UNEXPECTED_RESPONSE",
      error?.message ?? "The request could not be completed.",
      error?.next_action ?? "Refresh crypto activity and try again.",
      response.status,
    );
  }
  return payload as T;
}

export const cryptoAPI = {
  assets(): Promise<{ items: CryptoAsset[] }> {
    return request("/v1/crypto/assets");
  },
  portfolio(accessToken: string): Promise<{ items: CryptoBalance[] }> {
    return request("/v1/crypto/portfolio", accessToken);
  },
  conversions(accessToken: string): Promise<{ items: CryptoConversion[] }> {
    return request("/v1/crypto/conversions?page_size=50", accessToken);
  },
  convert(
    accessToken: string,
    body: {
      source_asset: string;
      destination_asset: string;
      source_amount: string;
      simulation_scenario: string;
    },
    idempotencyKey: string,
  ): Promise<CryptoConversion> {
    return request(
      "/v1/crypto/conversions",
      accessToken,
      { method: "POST", body: JSON.stringify(body) },
      idempotencyKey,
    );
  },
  depositAddresses(
    accessToken: string,
  ): Promise<{ items: CryptoDepositAddress[] }> {
    return request("/v1/crypto/deposit-addresses", accessToken);
  },
  createDepositAddress(
    accessToken: string,
    body: { asset: string; network: string },
    idempotencyKey: string,
  ): Promise<CryptoDepositAddress> {
    return request(
      "/v1/crypto/deposit-addresses",
      accessToken,
      { method: "POST", body: JSON.stringify(body) },
      idempotencyKey,
    );
  },
  deposits(accessToken: string): Promise<{ items: CryptoDeposit[] }> {
    return request("/v1/crypto/deposits?page_size=50", accessToken);
  },
  withdrawalAddresses(
    accessToken: string,
  ): Promise<{ items: CryptoWithdrawalAddress[] }> {
    return request("/v1/crypto/withdrawal-addresses", accessToken);
  },
  addWithdrawalAddress(
    accessToken: string,
    body: {
      asset: string;
      network: string;
      external_address: string;
      label: string;
      untrusted_device: boolean;
      recent_security_change: boolean;
      recent_recovery: boolean;
    },
    idempotencyKey: string,
  ): Promise<CryptoWithdrawalAddress> {
    return request(
      "/v1/crypto/withdrawal-addresses",
      accessToken,
      { method: "POST", body: JSON.stringify(body) },
      idempotencyKey,
    );
  },
  activateWithdrawalAddress(
    accessToken: string,
    addressID: string,
  ): Promise<CryptoWithdrawalAddress> {
    return request(
      `/v1/crypto/withdrawal-addresses/${encodeURIComponent(addressID)}/activate`,
      accessToken,
      { method: "POST" },
    );
  },
  withdrawals(accessToken: string): Promise<{ items: CryptoWithdrawal[] }> {
    return request("/v1/crypto/withdrawals?page_size=50", accessToken);
  },
  requestWithdrawal(
    accessToken: string,
    body: { address_id: string; quantity: string },
    idempotencyKey: string,
  ): Promise<CryptoWithdrawal> {
    return request(
      "/v1/crypto/withdrawals",
      accessToken,
      { method: "POST", body: JSON.stringify(body) },
      idempotencyKey,
    );
  },
};

export function positiveDecimal(value: string): boolean {
  return (
    /^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$/.test(value) &&
    !/^0(?:\.0+)?$/.test(value)
  );
}

export function conversionDisabledReason(
  sourceAsset: string,
  destinationAsset: string,
  amount: string,
): "SAME_ASSET" | "INVALID_AMOUNT" | null {
  if (sourceAsset === destinationAsset) return "SAME_ASSET";
  if (!positiveDecimal(amount)) return "INVALID_AMOUNT";
  return null;
}
