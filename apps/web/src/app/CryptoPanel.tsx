"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import { cryptoStatusCopy, enUS } from "@/i18n/en-US";
import {
  conversionDisabledReason,
  cryptoAPI,
  CryptoAPIError,
  CryptoAsset,
  CryptoBalance,
  CryptoConversion,
  CryptoDeposit,
  CryptoDepositAddress,
  CryptoWithdrawal,
  CryptoWithdrawalAddress,
  positiveDecimal,
} from "@/lib/crypto-api";

type BusyState =
  | "refresh"
  | "convert"
  | "deposit-address"
  | "withdrawal-address"
  | "activate"
  | "withdraw"
  | null;

type Props = {
  accessToken: string;
  onFinancialChange: () => Promise<void> | void;
};

const scenarios = [
  ["NORMAL", enUS.cryptoScenarioNormal],
  ["SPLIT", enUS.cryptoScenarioSplit],
  ["PARTIAL", enUS.cryptoScenarioPartial],
  ["TIMEOUT", enUS.cryptoScenarioTimeout],
  ["STALE", enUS.cryptoScenarioStale],
  ["VENUE_FAILURE", enUS.cryptoScenarioFailure],
  ["STABLECOIN_DEPEG", enUS.cryptoScenarioDepeg],
] as const;

export function CryptoPanel({ accessToken, onFinancialChange }: Props) {
  const [assets, setAssets] = useState<CryptoAsset[]>([]);
  const [balances, setBalances] = useState<CryptoBalance[]>([]);
  const [conversions, setConversions] = useState<CryptoConversion[]>([]);
  const [depositAddresses, setDepositAddresses] = useState<
    CryptoDepositAddress[]
  >([]);
  const [deposits, setDeposits] = useState<CryptoDeposit[]>([]);
  const [withdrawalAddresses, setWithdrawalAddresses] = useState<
    CryptoWithdrawalAddress[]
  >([]);
  const [withdrawals, setWithdrawals] = useState<CryptoWithdrawal[]>([]);
  const [sourceAsset, setSourceAsset] = useState("USD");
  const [destinationAsset, setDestinationAsset] = useState("BTC");
  const [sourceAmount, setSourceAmount] = useState("1000");
  const [scenario, setScenario] = useState("NORMAL");
  const [depositAsset, setDepositAsset] = useState("BTC");
  const [depositNetwork, setDepositNetwork] = useState("BITCOIN");
  const [withdrawalAsset, setWithdrawalAsset] = useState("BTC");
  const [withdrawalNetwork, setWithdrawalNetwork] = useState("BITCOIN");
  const [externalAddress, setExternalAddress] = useState("");
  const [addressLabel, setAddressLabel] = useState("");
  const [untrustedDevice, setUntrustedDevice] = useState(false);
  const [securityChange, setSecurityChange] = useState(false);
  const [recentRecovery, setRecentRecovery] = useState(false);
  const [withdrawalAddressID, setWithdrawalAddressID] = useState("");
  const [withdrawalQuantity, setWithdrawalQuantity] = useState("0.01");
  const [busy, setBusy] = useState<BusyState>("refresh");
  const [error, setError] = useState<CryptoAPIError | null>(null);
  const [notice, setNotice] = useState("");
  const [observedAt, setObservedAt] = useState(0);

  const refresh = useCallback(async () => {
    const [
      assetResult,
      portfolioResult,
      conversionResult,
      depositAddressResult,
      depositResult,
      withdrawalAddressResult,
      withdrawalResult,
    ] = await Promise.all([
      cryptoAPI.assets(),
      cryptoAPI.portfolio(accessToken),
      cryptoAPI.conversions(accessToken),
      cryptoAPI.depositAddresses(accessToken),
      cryptoAPI.deposits(accessToken),
      cryptoAPI.withdrawalAddresses(accessToken),
      cryptoAPI.withdrawals(accessToken),
    ]);
    setAssets(assetResult.items);
    setBalances(portfolioResult.items);
    setConversions(conversionResult.items);
    setDepositAddresses(depositAddressResult.items);
    setDeposits(depositResult.items);
    setWithdrawalAddresses(withdrawalAddressResult.items);
    setWithdrawals(withdrawalResult.items);
    setObservedAt(Date.now());
    const active = withdrawalAddressResult.items.find(
      (address) => address.status === "ACTIVE",
    );
    if (active) setWithdrawalAddressID(active.id);
    setBusy(null);
  }, [accessToken]);

  useEffect(() => {
    let active = true;
    Promise.resolve()
      .then(refresh)
      .catch((caught) => {
        if (active) setError(asCryptoError(caught));
      })
      .finally(() => {
        if (active) setBusy(null);
      });
    return () => {
      active = false;
    };
  }, [refresh]);

  async function refreshPanel() {
    setBusy("refresh");
    setError(null);
    try {
      await refresh();
    } catch (caught) {
      setError(asCryptoError(caught));
      setBusy(null);
    }
  }

  const depositNetworks = useMemo(
    () => assets.find((asset) => asset.symbol === depositAsset)?.networks ?? [],
    [assets, depositAsset],
  );
  const withdrawalNetworks = useMemo(
    () =>
      assets.find((asset) => asset.symbol === withdrawalAsset)?.networks ?? [],
    [assets, withdrawalAsset],
  );
  const activeWithdrawalAddresses = withdrawalAddresses.filter(
    (address) => address.status === "ACTIVE",
  );
  const conversionReason = conversionDisabledReason(
    sourceAsset,
    destinationAsset,
    sourceAmount,
  );

  async function convert(event: FormEvent) {
    event.preventDefault();
    if (conversionReason) return;
    setBusy("convert");
    setError(null);
    setNotice("");
    try {
      await cryptoAPI.convert(
        accessToken,
        {
          source_asset: sourceAsset,
          destination_asset: destinationAsset,
          source_amount: sourceAmount,
          simulation_scenario: scenario,
        },
        idempotencyKey("web-crypto-conversion"),
      );
      setNotice(enUS.cryptoConvertSuccess);
      await Promise.all([refresh(), onFinancialChange()]);
    } catch (caught) {
      setError(asCryptoError(caught));
      setBusy(null);
    }
  }

  async function createDepositAddress(event: FormEvent) {
    event.preventDefault();
    setBusy("deposit-address");
    setError(null);
    setNotice("");
    try {
      await cryptoAPI.createDepositAddress(
        accessToken,
        { asset: depositAsset, network: depositNetwork },
        idempotencyKey("web-crypto-deposit-address"),
      );
      await refresh();
    } catch (caught) {
      setError(asCryptoError(caught));
      setBusy(null);
    }
  }

  async function addWithdrawalAddress(event: FormEvent) {
    event.preventDefault();
    setBusy("withdrawal-address");
    setError(null);
    setNotice("");
    try {
      await cryptoAPI.addWithdrawalAddress(
        accessToken,
        {
          asset: withdrawalAsset,
          network: withdrawalNetwork,
          external_address: externalAddress,
          label: addressLabel,
          untrusted_device: untrustedDevice,
          recent_security_change: securityChange,
          recent_recovery: recentRecovery,
        },
        idempotencyKey("web-crypto-withdrawal-address"),
      );
      setExternalAddress("");
      setAddressLabel("");
      await refresh();
    } catch (caught) {
      setError(asCryptoError(caught));
      setBusy(null);
    }
  }

  async function activateAddress(address: CryptoWithdrawalAddress) {
    setBusy("activate");
    setError(null);
    try {
      await cryptoAPI.activateWithdrawalAddress(accessToken, address.id);
      await refresh();
    } catch (caught) {
      setError(asCryptoError(caught));
      setBusy(null);
    }
  }

  async function requestWithdrawal(event: FormEvent) {
    event.preventDefault();
    if (!withdrawalAddressID || !positiveDecimal(withdrawalQuantity)) return;
    setBusy("withdraw");
    setError(null);
    setNotice("");
    try {
      await cryptoAPI.requestWithdrawal(
        accessToken,
        { address_id: withdrawalAddressID, quantity: withdrawalQuantity },
        idempotencyKey("web-crypto-withdrawal"),
      );
      await Promise.all([refresh(), onFinancialChange()]);
    } catch (caught) {
      setError(asCryptoError(caught));
      setBusy(null);
    }
  }

  return (
    <section className="crypto-panel card" aria-labelledby="crypto-title">
      <div className="section-heading">
        <div>
          <p className="section-index">07 / CRYPTO</p>
          <h2 id="crypto-title">{enUS.cryptoTitle}</h2>
          <p>{enUS.cryptoDescription}</p>
        </div>
        <div className="crypto-heading-actions">
          <span className="mode-pill compact">
            <span aria-hidden="true" /> {enUS.simulated}
          </span>
          <button
            className="secondary"
            onClick={() => void refreshPanel()}
            disabled={busy === "refresh"}
          >
            {enUS.cryptoRefresh}
          </button>
        </div>
      </div>

      <p className="crypto-disclosure">{enUS.cryptoDisclosure}</p>
      {error && (
        <div className="crypto-error" role="alert">
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

      {busy === "refresh" ? (
        <div className="state-box" role="status">
          {enUS.cryptoLoading}
        </div>
      ) : (
        <>
          <section
            className="crypto-subsection"
            aria-labelledby="crypto-portfolio-title"
          >
            <h3 id="crypto-portfolio-title">{enUS.cryptoPortfolioTitle}</h3>
            {balances.length === 0 ? (
              <p className="empty-state">{enUS.cryptoPortfolioEmpty}</p>
            ) : (
              <div className="crypto-balance-grid">
                {balances.map((balance) => (
                  <article key={balance.asset}>
                    <strong>{balance.asset}</strong>
                    <dl>
                      <div>
                        <dt>{enUS.cryptoSettled}</dt>
                        <dd>{balance.settled}</dd>
                      </div>
                      <div>
                        <dt>{enUS.cryptoHeld}</dt>
                        <dd>{balance.held}</dd>
                      </div>
                      <div>
                        <dt>{enUS.cryptoFrozen}</dt>
                        <dd>{balance.frozen}</dd>
                      </div>
                    </dl>
                  </article>
                ))}
              </div>
            )}
          </section>

          <div className="crypto-work-grid">
            <section
              className="crypto-subsection"
              aria-labelledby="crypto-trade-title"
            >
              <h3 id="crypto-trade-title">{enUS.cryptoTradeTitle}</h3>
              <form onSubmit={convert} className="crypto-form">
                <label>
                  <span>{enUS.cryptoSourceAsset}</span>
                  <select
                    value={sourceAsset}
                    onChange={(event) => setSourceAsset(event.target.value)}
                  >
                    <option value="USD">USD</option>
                    {assets.map((asset) => (
                      <option key={asset.symbol} value={asset.symbol}>
                        {asset.symbol}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>{enUS.cryptoDestinationAsset}</span>
                  <select
                    value={destinationAsset}
                    onChange={(event) =>
                      setDestinationAsset(event.target.value)
                    }
                  >
                    <option value="USD">USD</option>
                    {assets.map((asset) => (
                      <option key={asset.symbol} value={asset.symbol}>
                        {asset.symbol}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>{enUS.cryptoSourceAmount}</span>
                  <input
                    inputMode="decimal"
                    value={sourceAmount}
                    onChange={(event) => setSourceAmount(event.target.value)}
                  />
                </label>
                <label>
                  <span>{enUS.cryptoScenario}</span>
                  <select
                    value={scenario}
                    onChange={(event) => setScenario(event.target.value)}
                  >
                    {scenarios.map(([value, label]) => (
                      <option key={value} value={value}>
                        {label}
                      </option>
                    ))}
                  </select>
                </label>
                {conversionReason && (
                  <p className="field-reason">
                    {conversionReason === "SAME_ASSET"
                      ? enUS.cryptoSameAssetDisabled
                      : enUS.cryptoInvalidAmountDisabled}
                  </p>
                )}
                <button
                  className="primary"
                  disabled={Boolean(conversionReason) || busy === "convert"}
                >
                  {busy === "convert"
                    ? enUS.cryptoConvertLoading
                    : enUS.cryptoConvertAction}
                </button>
              </form>
            </section>

            <section
              className="crypto-subsection"
              aria-labelledby="crypto-conversions-title"
            >
              <h3 id="crypto-conversions-title">
                {enUS.cryptoConversionsTitle}
              </h3>
              {conversions.length === 0 ? (
                <p className="empty-state">{enUS.cryptoConversionsEmpty}</p>
              ) : (
                <div className="crypto-conversion-list">
                  {conversions.slice(0, 5).map((conversion) => (
                    <ConversionCard
                      key={conversion.id}
                      conversion={conversion}
                    />
                  ))}
                </div>
              )}
            </section>
          </div>

          <div className="crypto-custody-grid">
            <section
              className="crypto-subsection"
              aria-labelledby="crypto-deposit-title"
            >
              <h3 id="crypto-deposit-title">{enUS.cryptoDepositTitle}</h3>
              <form
                className="crypto-form compact-form"
                onSubmit={createDepositAddress}
              >
                <AssetNetworkFields
                  assets={assets}
                  asset={depositAsset}
                  network={depositNetwork}
                  networks={depositNetworks.map((network) => network.code)}
                  setAsset={(next) => {
                    setDepositAsset(next);
                    setDepositNetwork(
                      assets.find((asset) => asset.symbol === next)?.networks[0]
                        ?.code ?? "",
                    );
                  }}
                  setNetwork={setDepositNetwork}
                />
                <button
                  className="secondary"
                  disabled={!depositNetwork || busy === "deposit-address"}
                >
                  {busy === "deposit-address"
                    ? enUS.cryptoDepositAddressLoading
                    : enUS.cryptoDepositAddressAction}
                </button>
              </form>
              {depositAddresses.length === 0 ? (
                <p className="empty-state">{enUS.cryptoDepositAddressEmpty}</p>
              ) : (
                <div className="crypto-address-list">
                  {depositAddresses.map((address) => (
                    <article key={address.id}>
                      <strong>
                        {address.asset} / {address.network}
                      </strong>
                      <code>{address.external_address}</code>
                      <small>{enUS.cryptoCopyNotice}</small>
                    </article>
                  ))}
                </div>
              )}
              {deposits.length === 0 ? (
                <p className="empty-state">{enUS.cryptoDepositActivityEmpty}</p>
              ) : (
                <div className="crypto-activity-list">
                  {deposits.slice(0, 5).map((deposit) => (
                    <p key={deposit.id}>
                      <strong>{deposit.asset}</strong> {deposit.quantity}
                      <StatusBadge status={deposit.status} />
                    </p>
                  ))}
                </div>
              )}
            </section>

            <section
              className="crypto-subsection"
              aria-labelledby="crypto-withdrawal-title"
            >
              <h3 id="crypto-withdrawal-title">{enUS.cryptoWithdrawalTitle}</h3>
              <form
                className="crypto-form compact-form"
                onSubmit={addWithdrawalAddress}
              >
                <AssetNetworkFields
                  assets={assets}
                  asset={withdrawalAsset}
                  network={withdrawalNetwork}
                  networks={withdrawalNetworks.map((network) => network.code)}
                  setAsset={(next) => {
                    setWithdrawalAsset(next);
                    setWithdrawalNetwork(
                      assets.find((asset) => asset.symbol === next)?.networks[0]
                        ?.code ?? "",
                    );
                  }}
                  setNetwork={setWithdrawalNetwork}
                />
                <label className="wide-field">
                  <span>{enUS.cryptoExternalAddressLabel}</span>
                  <input
                    value={externalAddress}
                    placeholder={enUS.cryptoAddressPlaceholder}
                    onChange={(event) => setExternalAddress(event.target.value)}
                    autoComplete="off"
                  />
                </label>
                <label className="wide-field">
                  <span>{enUS.cryptoAddressLabelLabel}</span>
                  <input
                    value={addressLabel}
                    placeholder={enUS.cryptoAddressLabelPlaceholder}
                    onChange={(event) => setAddressLabel(event.target.value)}
                    autoComplete="off"
                  />
                </label>
                <div className="crypto-checks wide-field">
                  <label>
                    <input
                      type="checkbox"
                      checked={untrustedDevice}
                      onChange={(event) =>
                        setUntrustedDevice(event.target.checked)
                      }
                    />
                    <span>{enUS.cryptoUntrustedDeviceLabel}</span>
                  </label>
                  <label>
                    <input
                      type="checkbox"
                      checked={securityChange}
                      onChange={(event) =>
                        setSecurityChange(event.target.checked)
                      }
                    />
                    <span>{enUS.cryptoSecurityChangeLabel}</span>
                  </label>
                  <label>
                    <input
                      type="checkbox"
                      checked={recentRecovery}
                      onChange={(event) =>
                        setRecentRecovery(event.target.checked)
                      }
                    />
                    <span>{enUS.cryptoRecoveryLabel}</span>
                  </label>
                </div>
                <button
                  className="secondary wide-field"
                  disabled={
                    externalAddress.length < 8 ||
                    !addressLabel ||
                    busy === "withdrawal-address"
                  }
                >
                  {busy === "withdrawal-address"
                    ? enUS.cryptoWithdrawalAddressLoading
                    : enUS.cryptoWithdrawalAddressAction}
                </button>
              </form>
              {withdrawalAddresses.length === 0 ? (
                <p className="empty-state">
                  {enUS.cryptoWithdrawalAddressEmpty}
                </p>
              ) : (
                <div className="crypto-address-list">
                  {withdrawalAddresses.map((address) => {
                    const canActivate =
                      address.status === "COOLING_OFF" &&
                      address.cooling_until !== null &&
                      Date.parse(address.cooling_until) <= observedAt;
                    return (
                      <article key={address.id}>
                        <div className="crypto-address-heading">
                          <strong>{address.label}</strong>
                          <StatusBadge status={address.status} />
                        </div>
                        <code>{address.external_address}</code>
                        <small>{address.cooling_reason}</small>
                        {address.cooling_until && (
                          <small>
                            {enUS.cryptoCoolingLabel}:{" "}
                            {formatTime(address.cooling_until)}
                          </small>
                        )}
                        {address.status === "COOLING_OFF" && (
                          <button
                            className="text-button"
                            disabled={!canActivate || busy === "activate"}
                            onClick={() => void activateAddress(address)}
                          >
                            {enUS.cryptoActivateAction}
                          </button>
                        )}
                      </article>
                    );
                  })}
                </div>
              )}

              <form
                className="crypto-form withdrawal-form"
                onSubmit={requestWithdrawal}
              >
                <label>
                  <span>{enUS.cryptoAddressLabel}</span>
                  <select
                    value={withdrawalAddressID}
                    onChange={(event) =>
                      setWithdrawalAddressID(event.target.value)
                    }
                  >
                    <option value="">-</option>
                    {activeWithdrawalAddresses.map((address) => (
                      <option key={address.id} value={address.id}>
                        {address.label} / {address.asset}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>{enUS.cryptoWithdrawalQuantityLabel}</span>
                  <input
                    inputMode="decimal"
                    value={withdrawalQuantity}
                    onChange={(event) =>
                      setWithdrawalQuantity(event.target.value)
                    }
                  />
                </label>
                {!withdrawalAddressID && (
                  <p className="field-reason wide-field">
                    {enUS.cryptoNoActiveAddress}
                  </p>
                )}
                <button
                  className="primary wide-field"
                  disabled={
                    !withdrawalAddressID ||
                    !positiveDecimal(withdrawalQuantity) ||
                    busy === "withdraw"
                  }
                >
                  {busy === "withdraw"
                    ? enUS.cryptoWithdrawalLoading
                    : enUS.cryptoWithdrawalAction}
                </button>
              </form>
              {withdrawals.length === 0 ? (
                <p className="empty-state">{enUS.cryptoWithdrawalEmpty}</p>
              ) : (
                <div className="crypto-activity-list">
                  {withdrawals.slice(0, 5).map((withdrawal) => (
                    <div key={withdrawal.id}>
                      <p>
                        <strong>{withdrawal.asset}</strong>{" "}
                        {withdrawal.quantity}
                        <StatusBadge status={withdrawal.status} />
                      </p>
                      <small>{withdrawal.next_action}</small>
                    </div>
                  ))}
                </div>
              )}
            </section>
          </div>
          <div className="crypto-policy-notes">
            <p>{enUS.cryptoCustodyNotice}</p>
            <p>{enUS.cryptoProhibitedProducts}</p>
          </div>
        </>
      )}
    </section>
  );
}

function ConversionCard({ conversion }: { conversion: CryptoConversion }) {
  return (
    <article className="crypto-conversion-card">
      <div>
        <strong>
          {conversion.source_asset} &rarr; {conversion.destination_asset}
        </strong>
        <StatusBadge status={conversion.status} />
      </div>
      {conversion.legs.map((leg) => (
        <div className="crypto-leg" key={leg.id}>
          <div>
            <span>
              {leg.sequence}. {leg.side} {leg.asset} / USD
            </span>
            <span className="quote-status">{enUS.cryptoQuoteLabel}</span>
          </div>
          <dl>
            <div>
              <dt>{enUS.cryptoFillLabel}</dt>
              <dd>{leg.filled_quantity}</dd>
            </div>
            <div>
              <dt>{enUS.cryptoFinalUSDLabel}</dt>
              <dd>${leg.final_customer_usd}</dd>
            </div>
            <div>
              <dt>{enUS.cryptoFeesLabel}</dt>
              <dd>
                ${leg.venue_fee_usd} + ${leg.platform_fee_usd}
              </dd>
            </div>
            <div>
              <dt>{enUS.cryptoImprovementLabel}</dt>
              <dd>${leg.price_improvement_usd}</dd>
            </div>
          </dl>
          <div className="crypto-children">
            {leg.children.map((child) => (
              <span key={child.id}>
                {child.venue} / {child.filled_quantity} /{" "}
                {cryptoStatusCopy[child.status] ?? child.status}
              </span>
            ))}
          </div>
        </div>
      ))}
      <small>
        {enUS.cryptoNextActionLabel}: {conversion.next_action}
      </small>
    </article>
  );
}

function AssetNetworkFields({
  assets,
  asset,
  network,
  networks,
  setAsset,
  setNetwork,
}: {
  assets: CryptoAsset[];
  asset: string;
  network: string;
  networks: string[];
  setAsset: (value: string) => void;
  setNetwork: (value: string) => void;
}) {
  return (
    <>
      <label>
        <span>{enUS.cryptoAssetLabel}</span>
        <select
          value={asset}
          onChange={(event) => setAsset(event.target.value)}
        >
          {assets.map((item) => (
            <option key={item.symbol} value={item.symbol}>
              {item.symbol}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>{enUS.cryptoNetworkLabel}</span>
        <select
          value={network}
          onChange={(event) => setNetwork(event.target.value)}
        >
          {networks.map((item) => (
            <option key={item} value={item}>
              {item}
            </option>
          ))}
        </select>
      </label>
    </>
  );
}

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`crypto-status status-${status.toLowerCase()}`}>
      {cryptoStatusCopy[status] ?? status}
    </span>
  );
}

function asCryptoError(caught: unknown): CryptoAPIError {
  return caught instanceof CryptoAPIError
    ? caught
    : new CryptoAPIError(
        "UNEXPECTED_ERROR",
        enUS.cryptoErrorFallback,
        enUS.cryptoErrorNextAction,
        500,
      );
}

function idempotencyKey(scope: string): string {
  const suffix =
    typeof globalThis.crypto?.randomUUID === "function"
      ? globalThis.crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${scope}.${suffix}`;
}

function formatTime(value: string): string {
  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
}
