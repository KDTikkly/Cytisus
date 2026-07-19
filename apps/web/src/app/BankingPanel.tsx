"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import {
  enUS,
  fundingStatusCopy,
  ownershipStatusCopy,
  withdrawalStatusCopy,
} from "@/i18n/en-US";
import {
  BankAccount,
  bankingAPI,
  BankingAPIError,
  BankWithdrawal,
  FundingTransfer,
  LinkBankAccount,
  positiveDecimal,
} from "@/lib/banking-api";

type BusyState = "refresh" | "link" | "fund" | "withdraw" | null;

const initialAccount: LinkBankAccount = {
  external_account_reference: "sim-bank-web-demo",
  rail_support: "BOTH",
  owner_relation: "SAME_NAME",
  risk_class: "STANDARD",
};

export function BankingPanel({
  accessToken,
  onFinancialChange,
}: {
  accessToken: string;
  onFinancialChange: () => Promise<void>;
}) {
  const [accounts, setAccounts] = useState<BankAccount[]>([]);
  const [funding, setFunding] = useState<FundingTransfer[]>([]);
  const [withdrawals, setWithdrawals] = useState<BankWithdrawal[]>([]);
  const [accountDraft, setAccountDraft] =
    useState<LinkBankAccount>(initialAccount);
  const [fundingAccountID, setFundingAccountID] = useState("");
  const [fundingRail, setFundingRail] = useState<"ACH" | "WIRE">("ACH");
  const [fundingAmount, setFundingAmount] = useState("100.00");
  const [withdrawalAccountID, setWithdrawalAccountID] = useState("");
  const [withdrawalAmount, setWithdrawalAmount] = useState("25.00");
  const [busy, setBusy] = useState<BusyState>("refresh");
  const [error, setError] = useState<BankingAPIError | null>(null);
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    const [nextAccounts, nextFunding, nextWithdrawals] = await Promise.all([
      bankingAPI.accounts(accessToken),
      bankingAPI.funding(accessToken),
      bankingAPI.withdrawals(accessToken),
    ]);
    setAccounts(nextAccounts.items);
    setFunding(nextFunding.items);
    setWithdrawals(nextWithdrawals.items);
    setFundingAccountID(
      (current) => current || nextAccounts.items[0]?.id || "",
    );
  }, [accessToken]);

  const handleError = useCallback((caught: unknown) => {
    setError(
      caught instanceof BankingAPIError
        ? caught
        : new BankingAPIError(
            "UNEXPECTED_ERROR",
            enUS.bankingErrorFallback,
            enUS.bankingErrorNextAction,
            500,
          ),
    );
    setNotice("");
  }, []);

  useEffect(() => {
    let active = true;
    Promise.resolve()
      .then(refresh)
      .catch((caught) => {
        if (active) handleError(caught);
      })
      .finally(() => {
        if (active) setBusy(null);
      });
    return () => {
      active = false;
    };
  }, [handleError, refresh]);

  const selectedFundingAccount = useMemo(
    () => accounts.find((account) => account.id === fundingAccountID) ?? null,
    [accounts, fundingAccountID],
  );
  const eligibleWithdrawalAccounts = useMemo(
    () =>
      accounts.filter(
        (account) =>
          account.owner_relation === "SAME_NAME" && account.status === "ACTIVE",
      ),
    [accounts],
  );
  const fundingDisabledReason = !selectedFundingAccount
    ? enUS.bankingNoAccount
    : !positiveDecimal(fundingAmount)
      ? enUS.bankingInvalidAmount
      : selectedFundingAccount.rail_support !== "BOTH" &&
          selectedFundingAccount.rail_support !== fundingRail
        ? enUS.bankingUnsupportedRail
        : fundingRail === "ACH" &&
            selectedFundingAccount.ownership_status !== "VERIFIED"
          ? enUS.bankingACHVerification
          : null;
  const withdrawalDisabledReason = !eligibleWithdrawalAccounts.length
    ? enUS.bankingNoAccount
    : !positiveDecimal(withdrawalAmount)
      ? enUS.bankingInvalidAmount
      : null;

  async function linkAccount(event: FormEvent) {
    event.preventDefault();
    setBusy("link");
    setError(null);
    setNotice("");
    try {
      const account = await bankingAPI.linkAccount(accessToken, accountDraft);
      await refresh();
      setFundingAccountID(account.id);
      setNotice(enUS.bankingLinkSuccess);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function initiateFunding(event: FormEvent) {
    event.preventDefault();
    if (!selectedFundingAccount || fundingDisabledReason) return;
    setBusy("fund");
    setError(null);
    setNotice("");
    try {
      await bankingAPI.initiateFunding(
        accessToken,
        {
          bank_account_id: selectedFundingAccount.id,
          rail: fundingRail,
          amount: fundingAmount,
        },
        idempotencyKey("web-funding"),
      );
      await Promise.all([refresh(), onFinancialChange()]);
      setNotice(enUS.fundingSuccess);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function requestWithdrawal(event: FormEvent) {
    event.preventDefault();
    if (withdrawalDisabledReason) return;
    setBusy("withdraw");
    setError(null);
    setNotice("");
    try {
      await bankingAPI.requestWithdrawal(
        accessToken,
        {
          amount: withdrawalAmount,
          ...(withdrawalAccountID
            ? { bank_account_id: withdrawalAccountID }
            : {}),
        },
        idempotencyKey("web-withdrawal"),
      );
      await Promise.all([refresh(), onFinancialChange()]);
      setNotice(enUS.withdrawalSuccess);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function refreshBanking() {
    setBusy("refresh");
    setError(null);
    try {
      await Promise.all([refresh(), onFinancialChange()]);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  return (
    <section className="banking-panel card" aria-labelledby="banking-title">
      <div className="section-heading">
        <div>
          <p className="section-index">06 / BANKING + CONTROLS</p>
          <h2 id="banking-title">{enUS.bankingTitle}</h2>
          <p className="section-description">{enUS.bankingDescription}</p>
        </div>
        <button
          className="secondary"
          onClick={refreshBanking}
          disabled={busy !== null}
        >
          {enUS.bankingRefresh}
        </button>
      </div>

      {error && (
        <div className="inline-error" role="alert" aria-live="assertive">
          <strong>{error.code}</strong>
          <p>{error.message}</p>
          <small>{error.nextAction}</small>
        </div>
      )}
      {notice && (
        <p className="notice" role="status" aria-live="polite">
          {notice}
        </p>
      )}

      {busy === "refresh" ? (
        <div className="loading-state" role="status" aria-live="polite">
          <span className="loading-mark" aria-hidden="true" />
          {enUS.bankingLoading}
        </div>
      ) : (
        <div className="banking-grid">
          <div className="banking-column">
            <h3>{enUS.bankingAccountTitle}</h3>
            <form className="stacked-form" onSubmit={linkAccount}>
              <label>
                <span>{enUS.bankingReferenceLabel}</span>
                <input
                  value={accountDraft.external_account_reference}
                  onChange={(event) =>
                    setAccountDraft((current) => ({
                      ...current,
                      external_account_reference: event.target.value,
                    }))
                  }
                  autoComplete="off"
                  required
                  minLength={3}
                  maxLength={128}
                />
                <small>{enUS.bankingReferenceHint}</small>
              </label>
              <label>
                <span>{enUS.bankingRailSupportLabel}</span>
                <select
                  value={accountDraft.rail_support}
                  onChange={(event) =>
                    setAccountDraft((current) => ({
                      ...current,
                      rail_support: event.target.value as
                        | "ACH"
                        | "WIRE"
                        | "BOTH",
                    }))
                  }
                >
                  <option value="BOTH">ACH + USD Wire</option>
                  <option value="ACH">ACH</option>
                  <option value="WIRE">USD Wire</option>
                </select>
              </label>
              <label>
                <span>{enUS.bankingRiskLabel}</span>
                <select
                  value={accountDraft.risk_class}
                  onChange={(event) =>
                    setAccountDraft((current) => ({
                      ...current,
                      risk_class: event.target.value as "STANDARD" | "ELEVATED",
                    }))
                  }
                >
                  <option value="STANDARD">{enUS.bankingRiskStandard}</option>
                  <option value="ELEVATED">{enUS.bankingRiskElevated}</option>
                </select>
              </label>
              <p className="policy-note">{enUS.withdrawalClosedLoopNotice}</p>
              <button className="primary" disabled={busy === "link"}>
                {busy === "link"
                  ? enUS.bankingLinkLoading
                  : enUS.bankingLinkAction}
              </button>
            </form>
            <p className="provider-note">{enUS.bankingProviderDriven}</p>
            {accounts.length === 0 ? (
              <p className="compact-empty">{enUS.bankingAccountEmpty}</p>
            ) : (
              <div className="banking-list">
                {accounts.map((account) => (
                  <article className="banking-item" key={account.id}>
                    <div className="banking-item-heading">
                      <strong>{account.external_account_reference}</strong>
                      <StatusPill
                        status={
                          ownershipStatusCopy[account.ownership_status] ??
                          account.ownership_status
                        }
                        tone={
                          account.ownership_status === "VERIFIED"
                            ? "positive"
                            : account.ownership_status === "FAILED"
                              ? "negative"
                              : "pending"
                        }
                      />
                    </div>
                    <small>
                      {account.owner_relation === "SAME_NAME"
                        ? enUS.bankingSameName
                        : enUS.bankingThirdParty}{" "}
                      · {account.rail_support}
                    </small>
                    {account.preferred_for_withdrawal && (
                      <span className="preferred-label">
                        {enUS.bankingPreferred}
                      </span>
                    )}
                    {account.cooling_until && (
                      <small>
                        {enUS.bankingCoolingLabel}:{" "}
                        {formatTime(account.cooling_until)}
                      </small>
                    )}
                  </article>
                ))}
              </div>
            )}
          </div>

          <div className="banking-column">
            <h3>{enUS.fundingTitle}</h3>
            <form className="stacked-form" onSubmit={initiateFunding}>
              <label>
                <span>{enUS.fundingBankLabel}</span>
                <select
                  value={fundingAccountID}
                  onChange={(event) => setFundingAccountID(event.target.value)}
                  disabled={accounts.length === 0}
                >
                  {accounts.length === 0 && <option value="">—</option>}
                  {accounts.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.external_account_reference}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>{enUS.fundingRailLabel}</span>
                <select
                  value={fundingRail}
                  onChange={(event) =>
                    setFundingRail(event.target.value as "ACH" | "WIRE")
                  }
                >
                  <option value="ACH">ACH</option>
                  <option value="WIRE">USD Wire</option>
                </select>
              </label>
              <label>
                <span>{enUS.fundingAmountLabel}</span>
                <input
                  inputMode="decimal"
                  value={fundingAmount}
                  onChange={(event) => setFundingAmount(event.target.value)}
                />
              </label>
              <p className="policy-note">{enUS.achUnsettledNotice}</p>
              {fundingDisabledReason && (
                <p className="field-reason">{fundingDisabledReason}</p>
              )}
              <button
                className="primary"
                disabled={Boolean(fundingDisabledReason) || busy === "fund"}
              >
                {busy === "fund" ? enUS.fundingLoading : enUS.fundingAction}
              </button>
            </form>
            {funding.length === 0 ? (
              <p className="compact-empty">{enUS.fundingEmpty}</p>
            ) : (
              <div className="banking-list">
                {funding.map((transfer) => (
                  <article className="banking-item" key={transfer.id}>
                    <div className="banking-item-heading">
                      <strong>
                        {transfer.rail} · ${transfer.amount}
                      </strong>
                      <StatusPill
                        status={
                          fundingStatusCopy[transfer.status] ?? transfer.status
                        }
                        tone={
                          transfer.settled
                            ? "positive"
                            : transfer.status === "RETURNED"
                              ? "negative"
                              : "pending"
                        }
                      />
                    </div>
                    <small>
                      {transfer.pending
                        ? enUS.bankingPending
                        : transfer.settled
                          ? enUS.bankingSettled
                          : (fundingStatusCopy[transfer.status] ??
                            transfer.status)}
                    </small>
                  </article>
                ))}
              </div>
            )}
          </div>

          <div className="banking-column">
            <h3>{enUS.withdrawalTitle}</h3>
            <form className="stacked-form" onSubmit={requestWithdrawal}>
              <label>
                <span>{enUS.withdrawalAccountLabel}</span>
                <select
                  value={withdrawalAccountID}
                  onChange={(event) =>
                    setWithdrawalAccountID(event.target.value)
                  }
                  disabled={eligibleWithdrawalAccounts.length === 0}
                >
                  <option value="">{enUS.withdrawalPreferredOption}</option>
                  {eligibleWithdrawalAccounts.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.external_account_reference}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>{enUS.withdrawalAmountLabel}</span>
                <input
                  inputMode="decimal"
                  value={withdrawalAmount}
                  onChange={(event) => setWithdrawalAmount(event.target.value)}
                />
              </label>
              <p className="policy-note">{enUS.withdrawalClosedLoopNotice}</p>
              {withdrawalDisabledReason && (
                <p className="field-reason">{withdrawalDisabledReason}</p>
              )}
              <button
                className="primary"
                disabled={
                  Boolean(withdrawalDisabledReason) || busy === "withdraw"
                }
              >
                {busy === "withdraw"
                  ? enUS.withdrawalLoading
                  : enUS.withdrawalAction}
              </button>
            </form>
            {withdrawals.length === 0 ? (
              <p className="compact-empty">{enUS.withdrawalEmpty}</p>
            ) : (
              <div className="banking-list">
                {withdrawals.map((withdrawal) => (
                  <article className="banking-item" key={withdrawal.id}>
                    <div className="banking-item-heading">
                      <strong>${withdrawal.amount}</strong>
                      <StatusPill
                        status={
                          withdrawalStatusCopy[withdrawal.status] ??
                          withdrawal.status
                        }
                        tone={withdrawalTone(withdrawal.status)}
                      />
                    </div>
                    {withdrawal.cooling_until && (
                      <small>
                        {enUS.bankingCoolingLabel}:{" "}
                        {formatTime(withdrawal.cooling_until)}
                      </small>
                    )}
                    <p className="next-action">
                      <span>{enUS.bankingNextActionLabel}</span>
                      {withdrawal.next_action}
                    </p>
                  </article>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  );
}

function StatusPill({
  status,
  tone,
}: {
  status: string;
  tone: "positive" | "pending" | "negative";
}) {
  return <span className={`bank-status ${tone}`}>{status}</span>;
}

function withdrawalTone(status: string): "positive" | "pending" | "negative" {
  if (["APPROVED", "SETTLED"].includes(status)) return "positive";
  if (["REJECTED", "RETURNED", "CANCELED", "NAME_MISMATCH"].includes(status)) {
    return "negative";
  }
  return "pending";
}

function formatTime(value: string): string {
  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
}

function idempotencyKey(prefix: string): string {
  return `${prefix}.${crypto.randomUUID()}`;
}
