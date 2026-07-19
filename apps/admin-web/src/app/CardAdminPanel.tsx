"use client";

import { FormEvent, useState } from "react";

import { enUS } from "@/i18n/en-US";
import {
  adminAPI,
  AdminActor,
  AdminAPIError,
  AdminCardDispute,
  AdminCardRecord,
  AdminCardStatement,
  CardReconciliation,
} from "@/lib/admin-api";

type Props = { actor: AdminActor };
type Busy =
  | "load"
  | "dispute"
  | "lifecycle"
  | "statement"
  | "reconciliation"
  | null;

export function CardAdminPanel({ actor }: Props) {
  const [disputes, setDisputes] = useState<AdminCardDispute[] | null>(null);
  const [runs, setRuns] = useState<CardReconciliation[]>([]);
  const [disputeID, setDisputeID] = useState("");
  const [accept, setAccept] = useState(true);
  const [reasonCode, setReasonCode] = useState("EVIDENCE_REVIEWED");
  const [cardID, setCardID] = useState("");
  const [nextStatus, setNextStatus] = useState("UNDER_REVIEW");
  const [lifecycleResult, setLifecycleResult] =
    useState<AdminCardRecord | null>(null);
  const [customerReference, setCustomerReference] = useState("");
  const today = new Date().toISOString().slice(0, 10);
  const [periodStart, setPeriodStart] = useState(today.slice(0, 8) + "01");
  const [periodEnd, setPeriodEnd] = useState(today);
  const [statement, setStatement] = useState<AdminCardStatement | null>(null);
  const [providerHold, setProviderHold] = useState("");
  const [providerReceivable, setProviderReceivable] = useState("");
  const [reconciliation, setReconciliation] =
    useState<CardReconciliation | null>(null);
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState<AdminAPIError | null>(null);
  const [notice, setNotice] = useState("");

  function fail(caught: unknown) {
    setError(
      caught instanceof AdminAPIError
        ? caught
        : new AdminAPIError(
            "UNEXPECTED_ERROR",
            enUS.cardAdminError,
            enUS.cardAdminNextAction,
            500,
          ),
    );
    setBusy(null);
  }

  async function load() {
    setBusy("load");
    setError(null);
    try {
      const [nextDisputes, nextRuns] = await Promise.all([
        adminAPI.cardDisputes(actor),
        adminAPI.cardReconciliations(actor, customerReference),
      ]);
      setDisputes(nextDisputes.items);
      setRuns(nextRuns.items);
      if (nextDisputes.items[0]) {
        setDisputeID(nextDisputes.items[0].id);
        setCustomerReference(
          (current) => current || nextDisputes.items[0].customer_reference,
        );
      }
      setBusy(null);
    } catch (caught) {
      fail(caught);
    }
  }

  async function resolve(event: FormEvent) {
    event.preventDefault();
    if (!disputeID) return;
    setBusy("dispute");
    setError(null);
    try {
      await adminAPI.resolveCardDispute(actor, disputeID, {
        accept,
        reason_code: reasonCode,
      });
      setNotice(enUS.cardAdminDisputeSuccess);
      await load();
    } catch (caught) {
      fail(caught);
    }
  }

  async function advance(event: FormEvent) {
    event.preventDefault();
    if (!cardID.trim()) return;
    setBusy("lifecycle");
    setError(null);
    try {
      setLifecycleResult(
        await adminAPI.advanceCard(actor, cardID, {
          next_status: nextStatus,
          reason_code: "OPERATIONS_UPDATE",
        }),
      );
      setNotice(enUS.cardAdminLifecycleSuccess);
      setBusy(null);
    } catch (caught) {
      fail(caught);
    }
  }

  async function generate(event: FormEvent) {
    event.preventDefault();
    if (!customerReference.trim()) return;
    setBusy("statement");
    setError(null);
    try {
      setStatement(
        await adminAPI.generateCardStatement(actor, {
          customer_reference: customerReference,
          period_start: periodStart,
          period_end: periodEnd,
        }),
      );
      setNotice(enUS.cardAdminStatementSuccess);
      setBusy(null);
    } catch (caught) {
      fail(caught);
    }
  }

  async function reconcile(event: FormEvent) {
    event.preventDefault();
    if (!customerReference.trim()) return;
    setBusy("reconciliation");
    setError(null);
    try {
      const result = await adminAPI.reconcileCard(actor, {
        customer_reference: customerReference,
        ...(providerHold.trim()
          ? { provider_hold_usd: providerHold.trim() }
          : {}),
        ...(providerReceivable.trim()
          ? { provider_receivable_usd: providerReceivable.trim() }
          : {}),
      });
      setReconciliation(result);
      setNotice(enUS.cardAdminReconciliationSuccess);
      await load();
    } catch (caught) {
      fail(caught);
    }
  }

  return (
    <section className="card card-admin" aria-labelledby="card-admin-title">
      <div className="section-heading">
        <div>
          <p className="section-index">05 / CARD</p>
          <h2 id="card-admin-title">{enUS.cardAdminTitle}</h2>
          <p>{enUS.cardAdminDescription}</p>
        </div>
        <button
          className="primary"
          onClick={() => void load()}
          disabled={busy !== null || !actor.id.trim()}
        >
          {busy === "load" ? enUS.cardAdminLoading : enUS.cardAdminLoad}
        </button>
      </div>

      {error && (
        <div className="card-admin-error" role="alert">
          <strong>{error.code}</strong>
          <span>{error.message}</span>
          <small>{error.nextAction}</small>
        </div>
      )}
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}

      <div className="card-admin-grid">
        <section>
          <h3>{enUS.cardAdminDisputesTitle}</h3>
          {disputes === null ? (
            <p className="empty-state">{enUS.cardAdminNotLoaded}</p>
          ) : disputes.length === 0 ? (
            <p className="empty-state">{enUS.cardAdminDisputesEmpty}</p>
          ) : (
            <div className="card-admin-list">
              {disputes.map((item) => (
                <button
                  key={item.id}
                  className={item.id === disputeID ? "selected" : ""}
                  onClick={() => {
                    setDisputeID(item.id);
                    setCustomerReference(item.customer_reference);
                  }}
                >
                  <span>
                    <strong>{item.amount_usd} USD</strong>
                    <small>{item.customer_reference}</small>
                  </span>
                  <span className="case-status">{item.status}</span>
                </button>
              ))}
            </div>
          )}
          <form className="stacked-form" onSubmit={resolve}>
            <label>
              <span>{enUS.cardAdminDecisionLabel}</span>
              <select
                value={accept ? "ACCEPT" : "REJECT"}
                onChange={(event) => setAccept(event.target.value === "ACCEPT")}
              >
                <option value="ACCEPT">{enUS.cardAdminAccept}</option>
                <option value="REJECT">{enUS.cardAdminReject}</option>
              </select>
            </label>
            <label>
              <span>{enUS.cardAdminReasonLabel}</span>
              <input
                value={reasonCode}
                onChange={(event) => setReasonCode(event.target.value)}
              />
            </label>
            <button className="primary" disabled={!disputeID || busy !== null}>
              {enUS.cardAdminResolve}
            </button>
          </form>
        </section>

        <section>
          <h3>{enUS.cardAdminLifecycleTitle}</h3>
          <form className="stacked-form" onSubmit={advance}>
            <label>
              <span>{enUS.cardAdminCardIDLabel}</span>
              <input
                value={cardID}
                onChange={(event) => setCardID(event.target.value)}
              />
            </label>
            <label>
              <span>{enUS.cardAdminNextStatusLabel}</span>
              <select
                value={nextStatus}
                onChange={(event) => setNextStatus(event.target.value)}
              >
                <option>UNDER_REVIEW</option>
                <option>ADDRESS_REVIEW</option>
                <option>APPROVED</option>
                <option>MANUFACTURING</option>
                <option>SHIPPED</option>
                <option>SHIPMENT_DELAYED</option>
                <option>DELIVERED</option>
                <option>REJECTED</option>
                <option>CANCELED</option>
              </select>
            </label>
            <button
              className="primary"
              disabled={!cardID.trim() || busy !== null}
            >
              {enUS.cardAdminAdvance}
            </button>
          </form>
          {lifecycleResult && (
            <dl className="card-admin-result">
              <Detail
                label={enUS.cardAdminResultLabel}
                value={`${lifecycleResult.card_type} •••• ${lifecycleResult.last4}`}
              />
              <Detail
                label={enUS.resultStatusLabel}
                value={lifecycleResult.status}
              />
              <Detail
                label={enUS.cardAdminWalletLabel}
                value={`${lifecycleResult.apple_wallet_status} / ${lifecycleResult.google_wallet_status}`}
              />
            </dl>
          )}
        </section>

        <section>
          <h3>{enUS.cardAdminStatementTitle}</h3>
          <form className="stacked-form" onSubmit={generate}>
            <label>
              <span>{enUS.customerLabel}</span>
              <input
                value={customerReference}
                onChange={(event) => setCustomerReference(event.target.value)}
              />
            </label>
            <label>
              <span>{enUS.cardAdminPeriodStart}</span>
              <input
                type="date"
                value={periodStart}
                onChange={(event) => setPeriodStart(event.target.value)}
              />
            </label>
            <label>
              <span>{enUS.cardAdminPeriodEnd}</span>
              <input
                type="date"
                value={periodEnd}
                onChange={(event) => setPeriodEnd(event.target.value)}
              />
            </label>
            <button
              className="primary"
              disabled={!customerReference.trim() || busy !== null}
            >
              {enUS.cardAdminGenerate}
            </button>
          </form>
          {statement && (
            <dl className="card-admin-result">
              <Detail
                label={enUS.cardAdminAmountDue}
                value={`${statement.amount_due_usd} USD`}
              />
              <Detail label={enUS.resultStatusLabel} value={statement.status} />
              <Detail
                label={enUS.cardAdminDueAt}
                value={new Date(statement.due_at).toLocaleString()}
              />
            </dl>
          )}
        </section>

        <section>
          <h3>{enUS.cardAdminReconciliationTitle}</h3>
          <p className="policy-note">{enUS.cardAdminReconciliationNotice}</p>
          <form className="stacked-form" onSubmit={reconcile}>
            <label>
              <span>{enUS.cardAdminProviderHold}</span>
              <input
                value={providerHold}
                onChange={(event) => setProviderHold(event.target.value)}
                placeholder={enUS.cardAdminOptional}
              />
            </label>
            <label>
              <span>{enUS.cardAdminProviderReceivable}</span>
              <input
                value={providerReceivable}
                onChange={(event) => setProviderReceivable(event.target.value)}
                placeholder={enUS.cardAdminOptional}
              />
            </label>
            <button
              className="primary"
              disabled={!customerReference.trim() || busy !== null}
            >
              {enUS.reconcileAction}
            </button>
          </form>
          {reconciliation && (
            <dl className="card-admin-result">
              <Detail
                label={enUS.ledgerAmountLabel}
                value={`${reconciliation.ledger_hold_usd} hold / ${reconciliation.ledger_receivable_usd} receivable`}
              />
              <Detail
                label={enUS.providerAmountLabel}
                value={`${reconciliation.provider_hold_usd} hold / ${reconciliation.provider_receivable_usd} receivable`}
              />
              <Detail
                label={enUS.differenceLabel}
                value={reconciliation.difference_usd}
              />
              <Detail
                label={enUS.resultStatusLabel}
                value={reconciliation.status}
              />
            </dl>
          )}
          {runs.length > 0 && (
            <small>
              {enUS.cardAdminRuns.replace("{count}", String(runs.length))}
            </small>
          )}
        </section>
      </div>
    </section>
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
