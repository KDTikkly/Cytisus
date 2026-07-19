export type BankAccount = {
  id: string;
  provider: string;
  external_account_reference: string;
  rail_support: "ACH" | "WIRE" | "BOTH";
  owner_relation: "SAME_NAME" | "THIRD_PARTY";
  ownership_status: "PENDING" | "VERIFIED" | "FAILED";
  status: "ACTIVE" | "BLOCKED" | "CLOSED";
  preferred_for_withdrawal: boolean;
  successfully_funded_at: string | null;
  cooling_until: string | null;
  created_at: string;
  mode: "SIMULATED";
};

export type FundingTransfer = {
  id: string;
  bank_account_id: string;
  rail: "ACH" | "WIRE";
  amount: string;
  currency: "USD";
  status: string;
  provider_transfer_id: string;
  reason_code: string | null;
  pending: boolean;
  settled: boolean;
  replayed: boolean;
  created_at: string;
  updated_at: string;
  mode: "SIMULATED";
};

export type BankWithdrawal = {
  id: string;
  bank_account_id: string;
  amount: string;
  currency: "USD";
  status: string;
  reason_code: string;
  next_action: string;
  compliance_case_id: string | null;
  cooling_until: string | null;
  provider_transfer_id: string;
  replayed: boolean;
  created_at: string;
  updated_at: string;
  mode: "SIMULATED";
};

export type LinkBankAccount = {
  external_account_reference: string;
  rail_support: "ACH" | "WIRE" | "BOTH";
  owner_relation: "SAME_NAME" | "THIRD_PARTY";
  risk_class: "STANDARD" | "ELEVATED";
};

export class BankingAPIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly nextAction: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "BankingAPIError";
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
    | {
        error?: {
          code?: string;
          message?: string;
          next_action?: string;
        };
      };
  if (!response.ok) {
    const error = "error" in payload ? payload.error : undefined;
    throw new BankingAPIError(
      error?.code ?? "UNEXPECTED_RESPONSE",
      error?.message ?? "The request could not be completed.",
      error?.next_action ?? "Refresh the page and try again.",
      response.status,
    );
  }
  return payload as T;
}

export const bankingAPI = {
  accounts(accessToken: string): Promise<{ items: BankAccount[] }> {
    return request("/v1/banks/accounts", accessToken);
  },
  linkAccount(
    accessToken: string,
    account: LinkBankAccount,
  ): Promise<BankAccount> {
    return request("/v1/banks/accounts", accessToken, {
      method: "POST",
      body: JSON.stringify(account),
    });
  },
  funding(accessToken: string): Promise<{ items: FundingTransfer[] }> {
    return request("/v1/transfers/funding?page_size=50", accessToken);
  },
  initiateFunding(
    accessToken: string,
    body: { bank_account_id: string; rail: "ACH" | "WIRE"; amount: string },
    idempotencyKey: string,
  ): Promise<FundingTransfer> {
    return request(
      "/v1/transfers/funding",
      accessToken,
      { method: "POST", body: JSON.stringify(body) },
      idempotencyKey,
    );
  },
  withdrawals(accessToken: string): Promise<{ items: BankWithdrawal[] }> {
    return request("/v1/transfers/withdrawals?page_size=50", accessToken);
  },
  requestWithdrawal(
    accessToken: string,
    body: { amount: string; bank_account_id?: string },
    idempotencyKey: string,
  ): Promise<BankWithdrawal> {
    return request(
      "/v1/transfers/withdrawals",
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
