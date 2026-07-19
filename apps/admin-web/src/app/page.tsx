"use client";

import { FormEvent, useMemo, useState } from "react";

import {
  caseStatusCopy,
  enUS,
  reconciliationStatusCopy,
  roleCopy,
} from "@/i18n/en-US";
import {
  adminAPI,
  AdminActor,
  AdminAPIError,
  BankReconciliation,
  ComplianceCase,
  ReviewProposal,
} from "@/lib/admin-api";

import { CardAdminPanel } from "./CardAdminPanel";

type ReviewRole =
  | "COMPLIANCE_ANALYST"
  | "RISK_ANALYST"
  | "OPERATIONS"
  | "ADMIN";
type BusyState = "cases" | "proposal" | "decision" | "reconciliation" | null;

const reviewRoles: ReviewRole[] = [
  "OPERATIONS",
  "COMPLIANCE_ANALYST",
  "RISK_ANALYST",
  "ADMIN",
];

export default function AdminHome() {
  const [maker, setMaker] = useState<AdminActor>({
    id: "sim-maker-a",
    role: "OPERATIONS",
  });
  const [checker, setChecker] = useState<AdminActor>({
    id: "sim-checker-b",
    role: "COMPLIANCE_ANALYST",
  });
  const [statusFilter, setStatusFilter] = useState("");
  const [cases, setCases] = useState<ComplianceCase[] | null>(null);
  const [selectedCaseID, setSelectedCaseID] = useState("");
  const [proposal, setProposal] = useState<ReviewProposal | null>(null);
  const [proposalID, setProposalID] = useState("");
  const [proposalAction, setProposalAction] = useState<
    "APPROVE_WITHDRAWAL" | "OVERRIDE_COOLING" | "REJECT_WITHDRAWAL"
  >("APPROVE_WITHDRAWAL");
  const [proposalReason, setProposalReason] = useState("EVIDENCE_REVIEWED");
  const [ticketReference, setTicketReference] = useState("SIM-TICKET-001");
  const [evidenceReference, setEvidenceReference] =
    useState("SIM-EVIDENCE-001");
  const [approve, setApprove] = useState(true);
  const [decisionReason, setDecisionReason] = useState(
    "Independent evidence review complete.",
  );
  const [reconciliationCustomer, setReconciliationCustomer] = useState("");
  const [providerAmount, setProviderAmount] = useState("");
  const [reconciliation, setReconciliation] =
    useState<BankReconciliation | null>(null);
  const [busy, setBusy] = useState<BusyState>(null);
  const [error, setError] = useState<AdminAPIError | null>(null);
  const [notice, setNotice] = useState("");

  const selectedCase = useMemo(
    () => cases?.find((item) => item.id === selectedCaseID) ?? null,
    [cases, selectedCaseID],
  );
  const samePerson =
    checker.id.trim() !== "" && checker.id.trim() === maker.id.trim();

  function handleError(caught: unknown) {
    setError(
      caught instanceof AdminAPIError
        ? caught
        : new AdminAPIError(
            "UNEXPECTED_ERROR",
            enUS.errorFallback,
            enUS.errorNextAction,
            500,
          ),
    );
    setNotice("");
  }

  async function loadCases() {
    setBusy("cases");
    setError(null);
    try {
      const response = await adminAPI.cases(maker, statusFilter);
      setCases(response.items);
      setSelectedCaseID((current) =>
        response.items.some((item) => item.id === current)
          ? current
          : (response.items[0]?.id ?? ""),
      );
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function createProposal(event: FormEvent) {
    event.preventDefault();
    if (
      !selectedCaseID ||
      !proposalReason.trim() ||
      !ticketReference.trim() ||
      !evidenceReference.trim()
    ) {
      setError(
        new AdminAPIError(
          "REVIEW_EVIDENCE_REQUIRED",
          enUS.proposalRequired,
          enUS.proposalRequired,
          400,
        ),
      );
      return;
    }
    setBusy("proposal");
    setError(null);
    setNotice("");
    try {
      const next = await adminAPI.propose(maker, selectedCaseID, {
        action: proposalAction,
        reason_code: proposalReason,
        ticket_reference: ticketReference,
        evidence_reference: evidenceReference,
      });
      setProposal(next);
      setProposalID(next.id);
      setNotice(enUS.proposalSuccess);
      await loadCasesAfterMutation();
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function decideProposal(event: FormEvent) {
    event.preventDefault();
    if (!proposalID.trim() || !decisionReason.trim()) {
      setError(
        new AdminAPIError(
          "DECISION_FIELDS_REQUIRED",
          enUS.decisionRequired,
          enUS.decisionRequired,
          400,
        ),
      );
      return;
    }
    if (samePerson) {
      setError(
        new AdminAPIError(
          "INDEPENDENT_APPROVAL_REQUIRED",
          enUS.differentCheckerRequired,
          enUS.differentCheckerRequired,
          409,
        ),
      );
      return;
    }
    setBusy("decision");
    setError(null);
    setNotice("");
    try {
      const next = await adminAPI.decide(checker, proposalID, {
        approve,
        decision_reason: decisionReason,
      });
      setProposal(next);
      setNotice(enUS.decisionSuccess);
      await loadCasesAfterMutation();
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function runReconciliation(event: FormEvent) {
    event.preventDefault();
    if (!reconciliationCustomer.trim()) {
      setError(
        new AdminAPIError(
          "CUSTOMER_REFERENCE_REQUIRED",
          enUS.reconciliationRequired,
          enUS.reconciliationRequired,
          400,
        ),
      );
      return;
    }
    setBusy("reconciliation");
    setError(null);
    setNotice("");
    try {
      const result = await adminAPI.reconcile(maker, {
        customer_reference: reconciliationCustomer,
        ...(providerAmount.trim()
          ? { provider_amount: providerAmount.trim() }
          : {}),
      });
      setReconciliation(result);
      setNotice(enUS.reconciliationSuccess);
      await loadCasesAfterMutation();
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function loadCasesAfterMutation() {
    try {
      const response = await adminAPI.cases(maker, statusFilter);
      setCases(response.items);
    } catch (caught) {
      handleError(caught);
    }
  }

  return (
    <main>
      <header className="masthead">
        <div>
          <p className="eyebrow">{enUS.eyebrow}</p>
          <h1>{enUS.title}</h1>
          <p className="description">{enUS.description}</p>
        </div>
        <span className="environment-badge">{enUS.environmentLabel}</span>
      </header>

      <p className="environment-notice">{enUS.environmentNotice}</p>

      {error && (
        <section className="error-banner" role="alert" aria-live="assertive">
          <div>
            <strong>{error.code}</strong>
            <p>{error.message}</p>
            <small>{error.nextAction}</small>
          </div>
          <button className="text-button" onClick={() => setError(null)}>
            {enUS.dismissError}
          </button>
        </section>
      )}
      {notice && (
        <p className="notice" role="status" aria-live="polite">
          {notice}
        </p>
      )}

      <section className="card queue" aria-labelledby="queue-title">
        <div className="section-heading">
          <div>
            <p className="section-index">01 / CASES</p>
            <h2 id="queue-title">{enUS.queueTitle}</h2>
            <p>{enUS.queueDescription}</p>
          </div>
          <button
            className="primary"
            onClick={loadCases}
            disabled={busy !== null || !maker.id.trim()}
          >
            {busy === "cases" ? enUS.queueLoading : enUS.queueLoadAction}
          </button>
        </div>
        <div className="identity-grid">
          <ActorFields actor={maker} onChange={setMaker} idPrefix="maker" />
          <label>
            <span>{enUS.queueStatusLabel}</span>
            <select
              value={statusFilter}
              onChange={(event) => setStatusFilter(event.target.value)}
            >
              <option value="">{enUS.queueAllOption}</option>
              {Object.entries(caseStatusCopy).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        </div>
        {busy === "cases" ? (
          <Loading copy={enUS.queueLoading} />
        ) : cases === null ? (
          <Empty copy={enUS.queueNotLoaded} />
        ) : cases.length === 0 ? (
          <Empty copy={enUS.queueEmpty} />
        ) : (
          <div className="case-list">
            {cases.map((item) => (
              <button
                className={`case-row ${selectedCaseID === item.id ? "selected" : ""}`}
                key={item.id}
                onClick={() => {
                  setSelectedCaseID(item.id);
                  setReconciliationCustomer(item.customer_reference);
                }}
                aria-pressed={selectedCaseID === item.id}
              >
                <span>
                  <strong>{item.case_type}</strong>
                  <small>{item.customer_reference}</small>
                </span>
                <span>{item.reason_code}</span>
                <CaseStatus status={item.status} />
              </button>
            ))}
          </div>
        )}
        {selectedCase && (
          <dl className="case-detail">
            <Detail label={enUS.selectedCaseLabel} value={selectedCase.id} />
            <Detail
              label={enUS.customerLabel}
              value={selectedCase.customer_reference}
            />
            <Detail label={enUS.caseTypeLabel} value={selectedCase.case_type} />
            <Detail label={enUS.reasonLabel} value={selectedCase.reason_code} />
            <Detail
              label={enUS.nextActionLabel}
              value={selectedCase.next_action}
            />
            <Detail
              label={enUS.policyLabel}
              value={selectedCase.policy_version}
            />
            <Detail
              label={enUS.updatedLabel}
              value={formatTime(selectedCase.updated_at)}
            />
          </dl>
        )}
      </section>

      <div className="review-grid">
        <section className="card review-card" aria-labelledby="maker-title">
          <p className="section-index">02 / MAKER</p>
          <h2 id="maker-title">{enUS.makerTitle}</h2>
          <p>{enUS.makerDescription}</p>
          <form className="stacked-form" onSubmit={createProposal}>
            <label>
              <span>{enUS.proposalActionLabel}</span>
              <select
                value={proposalAction}
                onChange={(event) =>
                  setProposalAction(
                    event.target.value as
                      | "APPROVE_WITHDRAWAL"
                      | "OVERRIDE_COOLING"
                      | "REJECT_WITHDRAWAL",
                  )
                }
              >
                <option value="APPROVE_WITHDRAWAL">
                  {enUS.proposalApprove}
                </option>
                <option value="OVERRIDE_COOLING">
                  {enUS.proposalOverride}
                </option>
                <option value="REJECT_WITHDRAWAL">{enUS.proposalReject}</option>
              </select>
            </label>
            <label>
              <span>{enUS.proposalReasonLabel}</span>
              <input
                value={proposalReason}
                onChange={(event) => setProposalReason(event.target.value)}
              />
            </label>
            <label>
              <span>{enUS.ticketLabel}</span>
              <input
                value={ticketReference}
                onChange={(event) => setTicketReference(event.target.value)}
              />
            </label>
            <label>
              <span>{enUS.evidenceLabel}</span>
              <input
                value={evidenceReference}
                onChange={(event) => setEvidenceReference(event.target.value)}
              />
            </label>
            <button
              className="primary"
              disabled={busy !== null || !selectedCaseID}
            >
              {busy === "proposal" ? enUS.proposing : enUS.proposeAction}
            </button>
          </form>
        </section>

        <section className="card review-card" aria-labelledby="checker-title">
          <p className="section-index">03 / CHECKER</p>
          <h2 id="checker-title">{enUS.checkerTitle}</h2>
          <p>{enUS.checkerDescription}</p>
          <form className="stacked-form" onSubmit={decideProposal}>
            <ActorFields
              actor={checker}
              onChange={setChecker}
              idPrefix="checker"
            />
            <label>
              <span>{enUS.proposalIDLabel}</span>
              <input
                value={proposalID}
                onChange={(event) => setProposalID(event.target.value)}
              />
            </label>
            <label>
              <span>{enUS.decisionLabel}</span>
              <select
                value={approve ? "APPROVE" : "REJECT"}
                onChange={(event) =>
                  setApprove(event.target.value === "APPROVE")
                }
              >
                <option value="APPROVE">{enUS.approveDecision}</option>
                <option value="REJECT">{enUS.rejectDecision}</option>
              </select>
            </label>
            <label>
              <span>{enUS.decisionReasonLabel}</span>
              <input
                value={decisionReason}
                onChange={(event) => setDecisionReason(event.target.value)}
              />
            </label>
            {samePerson && (
              <p className="disabled-reason">{enUS.differentCheckerRequired}</p>
            )}
            <button
              className="primary"
              disabled={busy !== null || samePerson || !proposalID.trim()}
            >
              {busy === "decision" ? enUS.deciding : enUS.decideAction}
            </button>
          </form>
          {proposal && (
            <p className="proposal-result">
              <strong>{proposal.status}</strong>
              <span>{proposal.id}</span>
            </p>
          )}
        </section>
      </div>

      <section
        className="card reconciliation"
        aria-labelledby="reconciliation-title"
      >
        <div>
          <p className="section-index">04 / RECONCILIATION</p>
          <h2 id="reconciliation-title">{enUS.reconciliationTitle}</h2>
          <p>{enUS.reconciliationDescription}</p>
        </div>
        <form className="stacked-form" onSubmit={runReconciliation}>
          <label>
            <span>{enUS.reconciliationCustomerLabel}</span>
            <input
              value={reconciliationCustomer}
              onChange={(event) =>
                setReconciliationCustomer(event.target.value)
              }
            />
          </label>
          <label>
            <span>{enUS.reconciliationProviderLabel}</span>
            <input
              inputMode="decimal"
              value={providerAmount}
              onChange={(event) => setProviderAmount(event.target.value)}
            />
            <small>{enUS.reconciliationProviderHint}</small>
          </label>
          <button className="primary" disabled={busy !== null}>
            {busy === "reconciliation"
              ? enUS.reconciling
              : enUS.reconcileAction}
          </button>
        </form>
        {reconciliation && (
          <dl className="reconciliation-result">
            <Detail
              label={enUS.resultStatusLabel}
              value={
                reconciliationStatusCopy[reconciliation.status] ??
                reconciliation.status
              }
            />
            <Detail
              label={enUS.ledgerAmountLabel}
              value={`$${reconciliation.ledger_amount}`}
            />
            <Detail
              label={enUS.providerAmountLabel}
              value={`$${reconciliation.provider_amount}`}
            />
            <Detail
              label={enUS.differenceLabel}
              value={`$${reconciliation.difference}`}
            />
          </dl>
        )}
      </section>

      <CardAdminPanel actor={maker} />

      <footer>{enUS.footer}</footer>
    </main>
  );
}

function ActorFields({
  actor,
  onChange,
  idPrefix,
}: {
  actor: AdminActor;
  onChange: (actor: AdminActor) => void;
  idPrefix: string;
}) {
  return (
    <>
      <label>
        <span>{enUS.actorIDLabel}</span>
        <input
          id={`${idPrefix}-id`}
          value={actor.id}
          onChange={(event) => onChange({ ...actor, id: event.target.value })}
          autoComplete="off"
        />
      </label>
      <label>
        <span>{enUS.actorRoleLabel}</span>
        <select
          id={`${idPrefix}-role`}
          value={actor.role}
          onChange={(event) =>
            onChange({ ...actor, role: event.target.value as ReviewRole })
          }
        >
          {reviewRoles.map((role) => (
            <option key={role} value={role}>
              {roleCopy[role]}
            </option>
          ))}
        </select>
      </label>
    </>
  );
}

function CaseStatus({ status }: { status: string }) {
  const tone = ["APPROVED", "CLOSED"].includes(status)
    ? "positive"
    : status === "REJECTED"
      ? "negative"
      : "pending";
  return (
    <span className={`case-status ${tone}`}>
      {caseStatusCopy[status] ?? status}
    </span>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function Loading({ copy }: { copy: string }) {
  return (
    <div className="state-box" role="status" aria-live="polite">
      <span className="loading-mark" aria-hidden="true" />
      {copy}
    </div>
  );
}

function Empty({ copy }: { copy: string }) {
  return <p className="state-box">{copy}</p>;
}

function formatTime(value: string): string {
  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
}
