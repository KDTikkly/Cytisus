export type AdminActor = {
  id: string;
  role:
    | "COMPLIANCE_ANALYST"
    | "RISK_ANALYST"
    | "OPERATIONS"
    | "ADMIN"
    | "AUDITOR"
    | "FINANCE";
};

export type ComplianceCase = {
  id: string;
  customer_reference: string;
  case_type: string;
  resource_type: string;
  resource_id: string;
  status: string;
  reason_code: string;
  next_action: string;
  policy_version: string;
  version: number;
  created_at: string;
  updated_at: string;
};

export type ReviewProposal = {
  id: string;
  case_id: string;
  action: string;
  maker_id: string;
  maker_role: string;
  reason_code: string;
  ticket_reference: string;
  evidence_reference: string;
  status: "PENDING" | "APPROVED" | "REJECTED";
  checker_id: string | null;
  checker_role: string | null;
  decision_reason: string | null;
  created_at: string;
  decided_at: string | null;
};

export type BankReconciliation = {
  id: string;
  status: "COMPLETED_WITHOUT_DIFFERENCE" | "COMPLETED_WITH_DIFFERENCES";
  ledger_amount: string;
  provider_amount: string;
  difference: string;
  compliance_case_id: string | null;
  started_at: string;
  completed_at: string;
};

export class AdminAPIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly nextAction: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "AdminAPIError";
  }
}

const baseURL = (
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080"
).replace(/\/$/, "");

async function request<T extends object>(
  path: string,
  actor: AdminActor,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  headers.set("X-Admin-ID", actor.id);
  headers.set("X-Admin-Role", actor.role);
  if (init.body) headers.set("Content-Type", "application/json");

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
    throw new AdminAPIError(
      error?.code ?? "UNEXPECTED_RESPONSE",
      error?.message ?? "The admin request could not be completed.",
      error?.next_action ?? "Refresh the case queue and try again.",
      response.status,
    );
  }
  return payload as T;
}

export const adminAPI = {
  cases(actor: AdminActor, status = ""): Promise<{ items: ComplianceCase[] }> {
    const query = status ? `?status=${encodeURIComponent(status)}` : "";
    return request(`/internal/v1/admin/compliance/cases${query}`, actor);
  },
  propose(
    actor: AdminActor,
    caseID: string,
    body: {
      action: "APPROVE_WITHDRAWAL" | "OVERRIDE_COOLING" | "REJECT_WITHDRAWAL";
      reason_code: string;
      ticket_reference: string;
      evidence_reference: string;
    },
  ): Promise<ReviewProposal> {
    return request(
      `/internal/v1/admin/compliance/cases/${encodeURIComponent(caseID)}/proposals`,
      actor,
      { method: "POST", body: JSON.stringify(body) },
    );
  },
  decide(
    actor: AdminActor,
    proposalID: string,
    body: { approve: boolean; decision_reason: string },
  ): Promise<ReviewProposal> {
    return request(
      `/internal/v1/admin/compliance/proposals/${encodeURIComponent(proposalID)}/decisions`,
      actor,
      { method: "POST", body: JSON.stringify(body) },
    );
  },
  reconcile(
    actor: AdminActor,
    body: { customer_reference: string; provider_amount?: string },
  ): Promise<BankReconciliation> {
    return request("/internal/v1/admin/reconciliation/banks", actor, {
      method: "POST",
      body: JSON.stringify(body),
    });
  },
};
