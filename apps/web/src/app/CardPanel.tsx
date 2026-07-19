"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import { enUS } from "@/i18n/en-US";
import {
  cardAPI,
  CardAPIError,
  CardAuthorization,
  CardCapture,
  CardDispute,
  cardIdempotencyKey,
  CardNotification,
  CardProfile,
  CardSpendingPower,
  CardStatement,
} from "@/lib/card-api";

type Props = {
  accessToken: string;
  onFinancialChange: () => Promise<void> | void;
};

type Busy =
  | "refresh"
  | "create"
  | "action"
  | "repayment"
  | "mandate"
  | "authorize"
  | "capture"
  | "dispute"
  | null;

export function CardPanel({ accessToken, onFinancialChange }: Props) {
  const [profile, setProfile] = useState<CardProfile | null>(null);
  const [power, setPower] = useState<CardSpendingPower | null>(null);
  const [authorizations, setAuthorizations] = useState<CardAuthorization[]>([]);
  const [captures, setCaptures] = useState<CardCapture[]>([]);
  const [disputes, setDisputes] = useState<CardDispute[]>([]);
  const [statements, setStatements] = useState<CardStatement[]>([]);
  const [notifications, setNotifications] = useState<CardNotification[]>([]);
  const [cardType, setCardType] = useState("VIRTUAL");
  const [repaymentMode, setRepaymentMode] = useState("CASH_ONLY");
  const [cardID, setCardID] = useState("");
  const [merchantName, setMerchantName] = useState("Cytisus Local Cafe");
  const [merchantAmount, setMerchantAmount] = useState("25");
  const [merchantCurrency, setMerchantCurrency] = useState("USD");
  const [entryMode, setEntryMode] = useState("NFC_SIMULATOR");
  const [scenario, setScenario] = useState("NORMAL");
  const [offline, setOffline] = useState(false);
  const [authorizationID, setAuthorizationID] = useState("");
  const [captureAmount, setCaptureAmount] = useState("25");
  const [captureFinal, setCaptureFinal] = useState(true);
  const [disputeCaptureID, setDisputeCaptureID] = useState("");
  const [disputeAmount, setDisputeAmount] = useState("5");
  const [busy, setBusy] = useState<Busy>("refresh");
  const [error, setError] = useState<CardAPIError | null>(null);
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    const [
      nextProfile,
      nextPower,
      auths,
      nextCaptures,
      nextDisputes,
      nextStatements,
      nextNotifications,
    ] = await Promise.all([
      cardAPI.profile(accessToken),
      cardAPI.spendingPower(accessToken),
      cardAPI.authorizations(accessToken),
      cardAPI.captures(accessToken),
      cardAPI.disputes(accessToken),
      cardAPI.statements(accessToken),
      cardAPI.notifications(accessToken),
    ]);
    setProfile(nextProfile);
    setPower(nextPower);
    setAuthorizations(auths.items);
    setCaptures(nextCaptures.items);
    setDisputes(nextDisputes.items);
    setStatements(nextStatements.items);
    setNotifications(nextNotifications.items);
    setRepaymentMode(nextProfile.repayment_mode);
    const activeCard = nextProfile.cards.find(
      (item) => item.status === "ACTIVE",
    );
    if (activeCard) setCardID(activeCard.id);
    if (auths.items[0]) setAuthorizationID(auths.items[0].id);
    if (nextCaptures.items[0]) setDisputeCaptureID(nextCaptures.items[0].id);
    setBusy(null);
  }, [accessToken]);

  useEffect(() => {
    let active = true;
    Promise.resolve()
      .then(refresh)
      .catch((caught) => {
        if (active) setError(asCardError(caught));
      })
      .finally(() => {
        if (active) setBusy(null);
      });
    return () => {
      active = false;
    };
  }, [refresh]);

  const activeCards = useMemo(
    () => profile?.cards.filter((item) => item.status === "ACTIVE") ?? [],
    [profile],
  );
  const terminalDisabled = !cardID || !positiveDecimal(merchantAmount);

  async function run(
    task: Busy,
    operation: () => Promise<unknown>,
    success: string,
    financial = false,
  ) {
    setBusy(task);
    setError(null);
    setNotice("");
    try {
      await operation();
      await Promise.all([
        refresh(),
        financial ? onFinancialChange() : Promise.resolve(),
      ]);
      setNotice(success);
    } catch (caught) {
      setError(asCardError(caught));
      setBusy(null);
    }
  }

  function createCard(event: FormEvent) {
    event.preventDefault();
    void run(
      "create",
      () =>
        cardAPI.createCard(
          accessToken,
          cardType,
          cardIdempotencyKey("web-card-create"),
        ),
      enUS.cardCreateSuccess,
    );
  }

  function cardAction(targetID: string, action: string) {
    void run(
      "action",
      () =>
        cardAPI.actOnCard(
          accessToken,
          targetID,
          action,
          cardIdempotencyKey("web-card-action"),
        ),
      enUS.cardActionSuccess,
    );
  }

  function configureRepayment(event: FormEvent) {
    event.preventDefault();
    void run(
      "repayment",
      () =>
        cardAPI.configureRepayment(
          accessToken,
          repaymentMode,
          cardIdempotencyKey("web-card-repayment"),
        ),
      enUS.cardRepaymentSuccess,
    );
  }

  function configureMandate() {
    void run(
      "mandate",
      async () => {
        await cardAPI.seedCollateral(
          accessToken,
          "TLT",
          "10",
          cardIdempotencyKey("web-card-collateral"),
        );
        await cardAPI.configureMandate(
          accessToken,
          cardIdempotencyKey("web-card-mandate"),
        );
      },
      enUS.cardMandateSuccess,
      true,
    );
  }

  function authorize(event: FormEvent) {
    event.preventDefault();
    if (terminalDisabled) return;
    const eventID = cardIdempotencyKey("terminal-auth");
    void run(
      "authorize",
      () =>
        cardAPI.authorize(accessToken, {
          external_event_id: eventID,
          card_id: cardID,
          merchant_name: merchantName,
          merchant_category_code: "5812",
          merchant_amount: merchantAmount,
          merchant_currency: merchantCurrency,
          entry_mode: offline ? "OFFLINE_SIMULATOR" : entryMode,
          offline,
          simulation_scenario: scenario,
        }),
      enUS.cardAuthorizationSuccess,
      true,
    );
  }

  function capture(event: FormEvent) {
    event.preventDefault();
    if (!authorizationID || !positiveDecimal(captureAmount)) return;
    void run(
      "capture",
      () =>
        cardAPI.capture(accessToken, {
          external_event_id: cardIdempotencyKey("terminal-capture"),
          authorization_id: authorizationID,
          merchant_amount: captureAmount,
          merchant_currency: merchantCurrency,
          final: captureFinal,
          simulation_scenario: scenario,
        }),
      enUS.cardCaptureSuccess,
      true,
    );
  }

  function openDispute(event: FormEvent) {
    event.preventDefault();
    if (!disputeCaptureID || !positiveDecimal(disputeAmount)) return;
    void run(
      "dispute",
      () =>
        cardAPI.openDispute(
          accessToken,
          disputeCaptureID,
          disputeAmount,
          cardIdempotencyKey("web-card-dispute"),
        ),
      enUS.cardDisputeSuccess,
    );
  }

  return (
    <section className="card-panel card" aria-labelledby="card-title">
      <div className="section-heading">
        <div>
          <p className="section-index">08 / CARD</p>
          <h2 id="card-title">{enUS.cardTitle}</h2>
          <p className="section-description">{enUS.cardDescription}</p>
        </div>
        <div className="card-heading-actions">
          <span className="mode-pill compact">
            <span aria-hidden="true" /> {enUS.simulated}
          </span>
          <button
            className="secondary"
            onClick={() =>
              void run("refresh", refresh, enUS.cardRefreshSuccess)
            }
            disabled={busy === "refresh"}
          >
            {enUS.cardRefresh}
          </button>
        </div>
      </div>

      <p className="card-disclosure">{enUS.cardDisclosure}</p>
      {error && (
        <div className="card-error" role="alert">
          <strong>{error.code}</strong>
          <span>{error.message}</span>
          <small>{error.nextAction}</small>
        </div>
      )}
      {notice && (
        <p className="notice" role="status" aria-live="polite">
          {notice}
        </p>
      )}

      {busy === "refresh" || !profile || !power ? (
        <div className="state-box" role="status">
          {enUS.cardLoading}
        </div>
      ) : (
        <>
          <div className="card-power-grid" aria-label={enUS.cardPowerTitle}>
            <PowerValue
              label={enUS.cardAvailable}
              value={power.available_usd}
            />
            <PowerValue
              label={enUS.cardCashEligible}
              value={power.cash_eligible_usd}
            />
            <PowerValue
              label={enUS.cardCollateralEligible}
              value={power.collateral_eligible_usd}
            />
            <PowerValue
              label={enUS.cardHolds}
              value={power.outstanding_holds_usd}
            />
            <PowerValue
              label={enUS.cardReceivable}
              value={power.receivable_usd}
            />
          </div>
          <p className="card-explanation">{power.primary_explanation}</p>

          <div className="card-work-grid">
            <section
              className="card-subsection"
              aria-labelledby="card-lifecycle-title"
            >
              <h3 id="card-lifecycle-title">{enUS.cardLifecycleTitle}</h3>
              <form className="stacked-form" onSubmit={createCard}>
                <label>
                  {enUS.cardTypeLabel}
                  <select
                    value={cardType}
                    onChange={(event) => setCardType(event.target.value)}
                  >
                    <option value="VIRTUAL">{enUS.cardVirtual}</option>
                    <option value="PLASTIC">{enUS.cardPlastic}</option>
                    <option value="METAL">{enUS.cardMetal}</option>
                  </select>
                </label>
                <button className="primary" disabled={busy === "create"}>
                  {enUS.cardCreateAction}
                </button>
              </form>
              {profile.cards.length === 0 ? (
                <p className="empty-state">{enUS.cardEmpty}</p>
              ) : (
                <div className="card-record-list">
                  {profile.cards.map((item) => (
                    <article key={item.id}>
                      <div>
                        <strong>
                          {item.display_name} •••• {item.last4}
                        </strong>
                        <Status value={item.status} />
                      </div>
                      <small>
                        {enUS.cardWalletState}: {item.apple_wallet_status} /{" "}
                        {item.google_wallet_status}
                      </small>
                      <div className="card-inline-actions">
                        {item.status === "CREATED" && (
                          <button
                            className="secondary"
                            onClick={() => cardAction(item.id, "ACTIVATE")}
                          >
                            {enUS.cardActivate}
                          </button>
                        )}
                        {item.status === "ACTIVE" && (
                          <button
                            className="secondary"
                            onClick={() => cardAction(item.id, "FREEZE")}
                          >
                            {enUS.cardFreeze}
                          </button>
                        )}
                        {item.status === "FROZEN" && (
                          <button
                            className="secondary"
                            onClick={() => cardAction(item.id, "UNFREEZE")}
                          >
                            {enUS.cardUnfreeze}
                          </button>
                        )}
                      </div>
                    </article>
                  ))}
                </div>
              )}
            </section>

            <section
              className="card-subsection"
              aria-labelledby="card-policy-title"
            >
              <h3 id="card-policy-title">{enUS.cardRepaymentTitle}</h3>
              <form className="stacked-form" onSubmit={configureRepayment}>
                <label>
                  {enUS.cardRepaymentLabel}
                  <select
                    value={repaymentMode}
                    onChange={(event) => setRepaymentMode(event.target.value)}
                  >
                    <option value="CASH_ONLY">CASH_ONLY</option>
                    <option value="CASH_THEN_AUTO_SELL">
                      CASH_THEN_AUTO_SELL
                    </option>
                    <option value="MONTHLY_STATEMENT">MONTHLY_STATEMENT</option>
                  </select>
                </label>
                <button className="primary" disabled={busy === "repayment"}>
                  {enUS.cardRepaymentAction}
                </button>
              </form>
              <button
                className="secondary full-button"
                onClick={configureMandate}
                disabled={busy === "mandate"}
              >
                {enUS.cardMandateAction}
              </button>
              <p className="policy-note">{enUS.cardProtectedSellNotice}</p>
              {power.drivers.map((driver) => (
                <div className="card-driver" key={driver.symbol}>
                  <strong>{driver.symbol}</strong>
                  <span>{driver.eligible_value_usd} USD</span>
                  <Status value={driver.quote_status} />
                </div>
              ))}
            </section>
          </div>

          <section
            className="card-subsection card-terminal"
            aria-labelledby="card-terminal-title"
          >
            <h3 id="card-terminal-title">{enUS.cardTerminalTitle}</h3>
            <p>{enUS.cardTerminalDescription}</p>
            {activeCards.length === 0 && (
              <p className="disabled-reason">{enUS.cardTerminalDisabled}</p>
            )}
            <div className="card-terminal-grid">
              <form className="stacked-form" onSubmit={authorize}>
                <label>
                  {enUS.cardSelectLabel}
                  <select
                    value={cardID}
                    onChange={(event) => setCardID(event.target.value)}
                  >
                    <option value="">{enUS.cardSelectPlaceholder}</option>
                    {activeCards.map((item) => (
                      <option value={item.id} key={item.id}>
                        •••• {item.last4}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  {enUS.cardMerchantLabel}
                  <input
                    value={merchantName}
                    onChange={(event) => setMerchantName(event.target.value)}
                  />
                </label>
                <div className="card-field-pair">
                  <label>
                    {enUS.cardAmountLabel}
                    <input
                      value={merchantAmount}
                      onChange={(event) =>
                        setMerchantAmount(event.target.value)
                      }
                      inputMode="decimal"
                    />
                  </label>
                  <label>
                    {enUS.cardCurrencyLabel}
                    <select
                      value={merchantCurrency}
                      onChange={(event) =>
                        setMerchantCurrency(event.target.value)
                      }
                    >
                      <option>USD</option>
                      <option>EUR</option>
                      <option>GBP</option>
                      <option>JPY</option>
                      <option>CAD</option>
                    </select>
                  </label>
                </div>
                <label>
                  {enUS.cardEntryModeLabel}
                  <select
                    value={entryMode}
                    onChange={(event) => setEntryMode(event.target.value)}
                  >
                    <option>NFC_SIMULATOR</option>
                    <option>ECOMMERCE_SIMULATOR</option>
                    <option>MANUAL_SIMULATOR</option>
                  </select>
                </label>
                <label>
                  {enUS.cardScenarioLabel}
                  <select
                    value={scenario}
                    onChange={(event) => setScenario(event.target.value)}
                  >
                    <option>NORMAL</option>
                    <option>DECLINE</option>
                    <option>PARTIAL</option>
                    <option>TIMEOUT</option>
                    <option>STALE</option>
                    <option>VENUE_FAILURE</option>
                    <option>PROTECTION_BREACH</option>
                  </select>
                </label>
                <label className="check-label">
                  <input
                    type="checkbox"
                    checked={offline}
                    onChange={(event) => setOffline(event.target.checked)}
                  />{" "}
                  {enUS.cardOfflineLabel}
                </label>
                <button
                  className="primary"
                  disabled={busy === "authorize" || terminalDisabled}
                >
                  {enUS.cardAuthorizeAction}
                </button>
              </form>
              <form className="stacked-form" onSubmit={capture}>
                <label>
                  {enUS.cardAuthorizationLabel}
                  <select
                    value={authorizationID}
                    onChange={(event) => setAuthorizationID(event.target.value)}
                  >
                    <option value="">
                      {enUS.cardAuthorizationPlaceholder}
                    </option>
                    {authorizations
                      .filter(
                        (item) =>
                          item.status === "APPROVED" ||
                          item.status === "PARTIALLY_CAPTURED",
                      )
                      .map((item) => (
                        <option value={item.id} key={item.id}>
                          {item.merchant_name} — {item.authorized_usd} USD
                        </option>
                      ))}
                  </select>
                </label>
                <label>
                  {enUS.cardCaptureAmountLabel}
                  <input
                    value={captureAmount}
                    onChange={(event) => setCaptureAmount(event.target.value)}
                    inputMode="decimal"
                  />
                </label>
                <label className="check-label">
                  <input
                    type="checkbox"
                    checked={captureFinal}
                    onChange={(event) => setCaptureFinal(event.target.checked)}
                  />{" "}
                  {enUS.cardFinalCaptureLabel}
                </label>
                <button
                  className="primary"
                  disabled={busy === "capture" || !authorizationID}
                >
                  {enUS.cardCaptureAction}
                </button>
              </form>
            </div>
          </section>

          <div className="card-work-grid">
            <section
              className="card-subsection"
              aria-labelledby="card-activity-title"
            >
              <h3 id="card-activity-title">{enUS.cardActivityTitle}</h3>
              {authorizations.length === 0 ? (
                <p className="empty-state">{enUS.cardActivityEmpty}</p>
              ) : (
                authorizations.map((item) => (
                  <article className="card-activity" key={item.id}>
                    <div>
                      <strong>{item.merchant_name}</strong>
                      <Status value={item.status} />
                    </div>
                    <small>
                      {item.entry_mode} · {item.merchant_amount}{" "}
                      {item.merchant_currency} · {item.authorized_usd} USD
                    </small>
                  </article>
                ))
              )}
              {captures.map((item) => (
                <article className="card-activity" key={item.id}>
                  <div>
                    <strong>
                      {enUS.cardCaptureLabel} {item.settled_usd} USD
                    </strong>
                    <Status value={item.status} />
                  </div>
                  <small>
                    {enUS.cardFXLabel} {item.clearing_fx_rate} ·{" "}
                    {enUS.cardTipLabel} {item.tip_usd} ·{" "}
                    {enUS.cardAutoSellLabel} {item.auto_sell_repaid_usd}
                  </small>
                </article>
              ))}
            </section>

            <section
              className="card-subsection"
              aria-labelledby="card-dispute-title"
            >
              <h3 id="card-dispute-title">{enUS.cardDisputeTitle}</h3>
              <form className="stacked-form" onSubmit={openDispute}>
                <label>
                  {enUS.cardCaptureSelectLabel}
                  <select
                    value={disputeCaptureID}
                    onChange={(event) =>
                      setDisputeCaptureID(event.target.value)
                    }
                  >
                    <option value="">
                      {enUS.cardCaptureSelectPlaceholder}
                    </option>
                    {captures.map((item) => (
                      <option value={item.id} key={item.id}>
                        {item.settled_usd} USD · {item.status}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  {enUS.cardDisputeAmountLabel}
                  <input
                    value={disputeAmount}
                    onChange={(event) => setDisputeAmount(event.target.value)}
                    inputMode="decimal"
                  />
                </label>
                <button
                  className="primary"
                  disabled={busy === "dispute" || !disputeCaptureID}
                >
                  {enUS.cardDisputeAction}
                </button>
              </form>
              {disputes.length === 0 ? (
                <p className="empty-state">{enUS.cardDisputeEmpty}</p>
              ) : (
                disputes.map((item) => (
                  <article className="card-activity" key={item.id}>
                    <div>
                      <strong>{item.amount_usd} USD</strong>
                      <Status value={item.status} />
                    </div>
                    <small>
                      {item.reason_code} ·{" "}
                      {item.outcome ?? enUS.cardReviewPending}
                    </small>
                  </article>
                ))
              )}
              <h3>{enUS.cardStatementsTitle}</h3>
              {statements.length === 0 ? (
                <p className="empty-state">{enUS.cardStatementsEmpty}</p>
              ) : (
                statements.map((item) => (
                  <article className="card-activity" key={item.id}>
                    <div>
                      <strong>{item.amount_due_usd} USD</strong>
                      <Status value={item.status} />
                    </div>
                    <small>
                      {item.period_start} — {item.period_end}
                    </small>
                  </article>
                ))
              )}
              <h3>{enUS.cardNotificationsTitle}</h3>
              {notifications.length === 0 ? (
                <p className="empty-state">{enUS.cardNotificationsEmpty}</p>
              ) : (
                notifications.slice(0, 5).map((item) => (
                  <article className="card-activity" key={item.id}>
                    <strong>{item.event_type}</strong>
                    <small>{item.delivery_status}</small>
                  </article>
                ))
              )}
            </section>
          </div>
        </>
      )}
    </section>
  );
}

function PowerValue({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>${value}</dd>
    </div>
  );
}

function Status({ value }: { value: string }) {
  return (
    <span className={`card-status status-${value.toLowerCase()}`}>
      {value.replaceAll("_", " ")}
    </span>
  );
}

function positiveDecimal(value: string): boolean {
  return (
    /^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/.test(value) &&
    !/^0(?:\.0+)?$/.test(value)
  );
}

function asCardError(caught: unknown): CardAPIError {
  return caught instanceof CardAPIError
    ? caught
    : new CardAPIError(
        "UNEXPECTED_ERROR",
        enUS.cardErrorFallback,
        enUS.cardErrorNextAction,
        500,
      );
}
