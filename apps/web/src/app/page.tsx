"use client";

import { FormEvent, useCallback, useState } from "react";

import {
  assetTypeCopy,
  enUS,
  orderStatusCopy,
  quoteStatusCopy,
} from "@/i18n/en-US";
import {
  actionDisabledReason,
  APIError,
  Instrument,
  Order,
  paperAPI,
  Portfolio,
  Quote,
  SubmitOrder,
} from "@/lib/paper-api";

import { BankingPanel } from "./BankingPanel";
import { CardPanel } from "./CardPanel";
import { CryptoPanel } from "./CryptoPanel";

type BusyState =
  | "register"
  | "search"
  | "quote"
  | "submit"
  | "portfolio"
  | "replay"
  | "cancel"
  | null;

const emptyOrder: SubmitOrder = {
  symbol: "",
  side: "BUY",
  order_type: "MARKET",
  time_in_force: "DAY",
  quantity: "0.5",
};

export default function Home() {
  const [fixtureID, setFixtureID] = useState<string>(enUS.fixtureDefault);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [accountID, setAccountID] = useState("");
  const [query, setQuery] = useState("");
  const [instruments, setInstruments] = useState<Instrument[]>([]);
  const [selected, setSelected] = useState<Instrument | null>(null);
  const [quote, setQuote] = useState<Quote | null>(null);
  const [orderDraft, setOrderDraft] = useState<SubmitOrder>(emptyOrder);
  const [orders, setOrders] = useState<Order[]>([]);
  const [portfolio, setPortfolio] = useState<Portfolio | null>(null);
  const [busy, setBusy] = useState<BusyState>(null);
  const [error, setError] = useState<APIError | null>(null);
  const [notice, setNotice] = useState("");

  const handleError = useCallback((caught: unknown) => {
    const next =
      caught instanceof APIError
        ? caught
        : new APIError("UNEXPECTED_ERROR", enUS.unexpectedError, 500);
    if (next.code === "AUTHENTICATION_REQUIRED") setAccessToken(null);
    setError(next);
    setNotice("");
  }, []);

  const refreshWorkspace = useCallback(async (token: string) => {
    const [nextPortfolio, nextOrders] = await Promise.all([
      paperAPI.portfolio(token),
      paperAPI.orders(token),
    ]);
    setPortfolio(nextPortfolio);
    setOrders(nextOrders.items);
  }, []);

  async function register(event: FormEvent) {
    event.preventDefault();
    setBusy("register");
    setError(null);
    try {
      const registration = await paperAPI.register(fixtureID);
      setAccessToken(registration.access_token);
      setAccountID(registration.account.id);
      const results = await paperAPI.search("");
      setInstruments(results.items);
      await refreshWorkspace(registration.access_token);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function search(event: FormEvent) {
    event.preventDefault();
    setBusy("search");
    setError(null);
    try {
      const results = await paperAPI.search(query);
      setInstruments(results.items);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function selectInstrument(instrument: Instrument) {
    setSelected(instrument);
    setOrderDraft((current) => ({ ...current, symbol: instrument.symbol }));
    setBusy("quote");
    setError(null);
    try {
      setQuote(await paperAPI.quote(instrument.symbol));
    } catch (caught) {
      setQuote(null);
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function submitOrder(event: FormEvent) {
    event.preventDefault();
    if (!accessToken || !selected) return;
    setBusy("submit");
    setError(null);
    setNotice("");
    try {
      const payload = {
        ...orderDraft,
        symbol: selected.symbol,
        limit_price:
          orderDraft.order_type === "LIMIT" ? orderDraft.limit_price : null,
      };
      await paperAPI.submit(accessToken, payload, idempotencyKey("web-order"));
      await refreshWorkspace(accessToken);
      setNotice(enUS.orderSuccess);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function orderAction(order: Order, action: "replay" | "cancel") {
    if (!accessToken) return;
    setBusy(action);
    setError(null);
    setNotice("");
    try {
      if (action === "replay") {
        await paperAPI.replay(
          accessToken,
          order.id,
          idempotencyKey("web-replay"),
        );
        setNotice(enUS.replaySuccess);
      } else {
        await paperAPI.cancel(
          accessToken,
          order.id,
          idempotencyKey("web-cancel"),
        );
        setNotice(enUS.cancelSuccess);
      }
      await refreshWorkspace(accessToken);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  async function refreshPortfolio() {
    if (!accessToken) return;
    setBusy("portfolio");
    setError(null);
    try {
      await refreshWorkspace(accessToken);
    } catch (caught) {
      handleError(caught);
    } finally {
      setBusy(null);
    }
  }

  const disabledReason = actionDisabledReason(
    selected,
    orderDraft.quantity,
    orderDraft.order_type,
    orderDraft.limit_price,
  );
  const disabledCopy = disabledReason
    ? {
        NO_INSTRUMENT: enUS.disabledNoInstrument,
        VIEW_ONLY: enUS.disabledViewOnly,
        NO_QUANTITY: enUS.disabledNoQuantity,
        NO_LIMIT_PRICE: enUS.disabledNoLimitPrice,
      }[disabledReason]
    : null;

  return (
    <main>
      <header className="masthead">
        <div>
          <a className="brand" href="#top" aria-label={enUS.appName}>
            {enUS.brandMark.split("/")[0]}
            <span>/{enUS.brandMark.split("/")[1]}</span>
          </a>
          <p className="eyebrow">{enUS.eyebrow}</p>
        </div>
        <div className="mode-pill">
          <span aria-hidden="true" /> {enUS.simulated}
        </div>
      </header>

      <section className="hero" id="top" aria-labelledby="page-title">
        <div>
          <h1 id="page-title">{enUS.title}</h1>
          <p className="description">{enUS.description}</p>
        </div>
        <p className="disclosure">{enUS.simulationDisclosure}</p>
      </section>

      {error && (
        <section className="error-banner" role="alert" aria-live="assertive">
          <div>
            <strong>{error.code}</strong>
            <p>{error.message}</p>
            <small>{enUS.retryHint}</small>
          </div>
          <button className="icon-button" onClick={() => setError(null)}>
            <span aria-hidden="true">×</span>
            <span className="sr-only">{enUS.dismissError}</span>
          </button>
        </section>
      )}

      {notice && (
        <p className="notice" role="status" aria-live="polite">
          {notice}
        </p>
      )}

      {!accessToken ? (
        <section
          className="registration card"
          aria-labelledby="registration-title"
        >
          <div>
            <p className="section-index">01 / FIXTURE</p>
            <h2 id="registration-title">{enUS.registrationTitle}</h2>
            <p>{enUS.registrationDescription}</p>
          </div>
          <form onSubmit={register}>
            <label htmlFor="fixture-id">{enUS.fixtureLabel}</label>
            <input
              id="fixture-id"
              value={fixtureID}
              onChange={(event) => setFixtureID(event.target.value)}
              aria-describedby="fixture-hint"
              autoComplete="off"
              required
            />
            <small id="fixture-hint">{enUS.fixtureHint}</small>
            <button className="primary" disabled={busy === "register"}>
              {busy === "register"
                ? enUS.registrationLoading
                : enUS.registerAction}
            </button>
          </form>
        </section>
      ) : (
        <div className="workspace" aria-label={enUS.workspaceLabel}>
          <div className="session-bar">
            <span>{accountID.slice(0, 8)}</span>
            <button
              className="text-button"
              onClick={() => {
                setAccessToken(null);
                setPortfolio(null);
                setOrders([]);
              }}
            >
              {enUS.resetSession}
            </button>
          </div>

          <section className="market-panel card" aria-labelledby="search-title">
            <div className="section-heading">
              <div>
                <p className="section-index">02 / MARKET</p>
                <h2 id="search-title">{enUS.searchTitle}</h2>
              </div>
              <form className="search" onSubmit={search} role="search">
                <label className="sr-only" htmlFor="instrument-search">
                  {enUS.searchLabel}
                </label>
                <input
                  id="instrument-search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder={enUS.searchPlaceholder}
                />
                <button disabled={busy === "search"}>
                  {enUS.searchAction}
                </button>
              </form>
            </div>

            {busy === "search" ? (
              <LoadingState copy={enUS.searchLoading} />
            ) : instruments.length === 0 ? (
              <EmptyState copy={enUS.searchEmpty} />
            ) : (
              <div className="instrument-list">
                {instruments.map((instrument) => (
                  <button
                    className={`instrument-row ${selected?.id === instrument.id ? "selected" : ""}`}
                    key={instrument.id}
                    onClick={() => selectInstrument(instrument)}
                    aria-pressed={selected?.id === instrument.id}
                  >
                    <span className="symbol-block">
                      <strong>{instrument.symbol}</strong>
                      <small>{instrument.display_name}</small>
                    </span>
                    <span>{assetTypeCopy[instrument.asset_type]}</span>
                    <span>{instrument.primary_exchange}</span>
                    <span
                      className={
                        instrument.capability.paper_tradable
                          ? "eligible"
                          : "view-only"
                      }
                    >
                      {instrument.capability.paper_tradable
                        ? enUS.tradableLabel
                        : enUS.viewOnlyLabel}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </section>

          <div className="trade-grid">
            <section className="quote-panel card" aria-labelledby="quote-title">
              <p className="section-index">03 / QUOTE</p>
              <h2 id="quote-title">{enUS.quoteTitle}</h2>
              {busy === "quote" ? (
                <LoadingState copy={enUS.quoteLoading} />
              ) : quote && selected ? (
                <>
                  <div className="quote-topline">
                    <div>
                      <strong>{quote.symbol}</strong>
                      <small>{selected.display_name}</small>
                    </div>
                    <QuoteBadge status={quote.status} />
                  </div>
                  <dl className="quote-grid">
                    <QuoteValue label={enUS.bidLabel} value={quote.bid} />
                    <QuoteValue label={enUS.askLabel} value={quote.ask} />
                    <QuoteValue label={enUS.lastLabel} value={quote.last} />
                  </dl>
                  <dl className="metadata-list">
                    <div>
                      <dt>{enUS.marketStatusLabel}</dt>
                      <dd>{quote.market_status}</dd>
                    </div>
                    <div>
                      <dt>{enUS.observedLabel}</dt>
                      <dd>{formatTime(quote.observed_at)}</dd>
                    </div>
                  </dl>
                  {!selected.capability.paper_tradable && (
                    <p className="disabled-reason">
                      {enUS.disabledASSET_TYPE_VIEW_ONLY}
                    </p>
                  )}
                </>
              ) : (
                <EmptyState copy={enUS.disabledNoInstrument} />
              )}
            </section>

            <section
              className="ticket-panel card"
              aria-labelledby="order-title"
            >
              <p className="section-index">04 / ORDER</p>
              <h2 id="order-title">{enUS.orderTitle}</h2>
              <form onSubmit={submitOrder}>
                <div
                  className="segmented"
                  role="group"
                  aria-label={enUS.sideLabel}
                >
                  {(["BUY", "SELL"] as const).map((side) => (
                    <button
                      type="button"
                      key={side}
                      aria-pressed={orderDraft.side === side}
                      onClick={() =>
                        setOrderDraft((current) => ({ ...current, side }))
                      }
                    >
                      {side === "BUY" ? enUS.buyOption : enUS.sellOption}
                    </button>
                  ))}
                </div>
                <div className="form-grid">
                  <label>
                    <span>{enUS.orderTypeLabel}</span>
                    <select
                      value={orderDraft.order_type}
                      onChange={(event) =>
                        setOrderDraft((current) => ({
                          ...current,
                          order_type: event.target.value as "MARKET" | "LIMIT",
                        }))
                      }
                    >
                      <option value="MARKET">{enUS.marketOption}</option>
                      <option value="LIMIT">{enUS.limitOption}</option>
                    </select>
                  </label>
                  <label>
                    <span>{enUS.timeInForceLabel}</span>
                    <select
                      value={orderDraft.time_in_force}
                      onChange={(event) =>
                        setOrderDraft((current) => ({
                          ...current,
                          time_in_force: event.target.value as "DAY" | "GTC",
                        }))
                      }
                    >
                      <option value="DAY">{enUS.dayOption}</option>
                      <option value="GTC">{enUS.gtcOption}</option>
                    </select>
                  </label>
                  <label>
                    <span>{enUS.quantityLabel}</span>
                    <input
                      inputMode="decimal"
                      value={orderDraft.quantity}
                      placeholder={enUS.quantityPlaceholder}
                      onChange={(event) =>
                        setOrderDraft((current) => ({
                          ...current,
                          quantity: event.target.value,
                        }))
                      }
                    />
                  </label>
                  {orderDraft.order_type === "LIMIT" && (
                    <label>
                      <span>{enUS.limitPriceLabel}</span>
                      <input
                        inputMode="decimal"
                        value={orderDraft.limit_price ?? ""}
                        placeholder={enUS.limitPricePlaceholder}
                        onChange={(event) =>
                          setOrderDraft((current) => ({
                            ...current,
                            limit_price: event.target.value,
                          }))
                        }
                      />
                    </label>
                  )}
                </div>
                {disabledCopy && <p className="field-reason">{disabledCopy}</p>}
                <button
                  className="primary"
                  disabled={Boolean(disabledReason) || busy === "submit"}
                >
                  {busy === "submit" ? enUS.submitLoading : enUS.submitOrder}
                </button>
              </form>
            </section>
          </div>

          <section
            className="portfolio-panel card"
            aria-labelledby="portfolio-title"
          >
            <div className="section-heading">
              <div>
                <p className="section-index">05 / LEDGER + POSITIONS</p>
                <h2 id="portfolio-title">{enUS.portfolioTitle}</h2>
              </div>
              <button
                className="secondary"
                onClick={refreshPortfolio}
                disabled={busy === "portfolio"}
              >
                {enUS.refreshAction}
              </button>
            </div>
            {busy === "portfolio" || !portfolio ? (
              <LoadingState copy={enUS.portfolioLoading} />
            ) : (
              <>
                <dl className="cash-grid">
                  <CashValue
                    label={enUS.settledLabel}
                    value={portfolio.cash.settled}
                  />
                  <CashValue
                    label={enUS.withdrawableLabel}
                    value={portfolio.cash.withdrawable}
                  />
                  <CashValue
                    label={enUS.provisionalLabel}
                    value={portfolio.cash.provisional_buying_power}
                    provisional
                  />
                  <CashValue
                    label={enUS.totalBuyingPowerLabel}
                    value={portfolio.cash.total_buying_power}
                  />
                </dl>
                <h3>{enUS.positionsTitle}</h3>
                {portfolio.positions.length === 0 ? (
                  <EmptyState copy={enUS.positionsEmpty} />
                ) : (
                  <div className="data-table" role="table">
                    {portfolio.positions.map((position) => (
                      <div
                        className="data-row"
                        role="row"
                        key={position.symbol}
                      >
                        <strong role="cell">{position.symbol}</strong>
                        <span role="cell">{position.quantity}</span>
                        <span role="cell">${position.average_cost}</span>
                        <span role="cell">${position.market_value}</span>
                        <QuoteBadge status={position.quote_status} />
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </section>

          <BankingPanel
            accessToken={accessToken}
            onFinancialChange={() => refreshWorkspace(accessToken)}
          />

          <CryptoPanel
            accessToken={accessToken}
            onFinancialChange={() => refreshWorkspace(accessToken)}
          />

          <CardPanel
            accessToken={accessToken}
            onFinancialChange={() => refreshWorkspace(accessToken)}
          />

          <section className="orders-panel card" aria-labelledby="orders-title">
            <p className="section-index">09 / ACTIVITY</p>
            <h2 id="orders-title">{enUS.ordersTitle}</h2>
            {orders.length === 0 ? (
              <EmptyState copy={enUS.ordersEmpty} />
            ) : (
              <div className="order-list">
                {orders.map((order) => {
                  const actionable = ["OPEN", "PARTIALLY_FILLED"].includes(
                    order.status,
                  );
                  return (
                    <article className="order-row" key={order.id}>
                      <div>
                        <strong>
                          {order.side} {order.symbol}
                        </strong>
                        <small>
                          {order.order_type} · {order.time_in_force} ·{" "}
                          {enUS.filledLabel} {order.filled_quantity}/
                          {order.quantity}
                        </small>
                      </div>
                      <div className="order-state">
                        <QuoteBadge status={order.quote_status} />
                        <span
                          className={`order-status ${order.status.toLowerCase()}`}
                        >
                          {orderStatusCopy[order.status]}
                        </span>
                      </div>
                      {actionable && (
                        <div className="order-actions">
                          <button
                            className="secondary"
                            disabled={busy === "replay"}
                            onClick={() => orderAction(order, "replay")}
                          >
                            {busy === "replay"
                              ? enUS.replayLoading
                              : enUS.replayAction}
                          </button>
                          <button
                            className="text-button"
                            disabled={busy === "cancel"}
                            onClick={() => orderAction(order, "cancel")}
                          >
                            {enUS.cancelAction}
                          </button>
                        </div>
                      )}
                    </article>
                  );
                })}
              </div>
            )}
          </section>
        </div>
      )}

      <footer>{enUS.footer}</footer>
    </main>
  );
}

function LoadingState({ copy }: { copy: string }) {
  return (
    <div className="loading-state" role="status" aria-live="polite">
      <span className="loading-mark" aria-hidden="true" />
      {copy}
    </div>
  );
}

function EmptyState({ copy }: { copy: string }) {
  return <p className="empty-state">{copy}</p>;
}

function QuoteBadge({ status }: { status: Quote["status"] }) {
  return (
    <span className={`quote-badge ${status.toLowerCase()}`}>
      {quoteStatusCopy[status]}
    </span>
  );
}

function QuoteValue({ label, value }: { label: string; value: string | null }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value ? `$${value}` : "—"}</dd>
    </div>
  );
}

function CashValue({
  label,
  value,
  provisional = false,
}: {
  label: string;
  value: string;
  provisional?: boolean;
}) {
  return (
    <div className={provisional ? "provisional" : ""}>
      <dt>{label}</dt>
      <dd>${value}</dd>
    </div>
  );
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
