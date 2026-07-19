import Foundation
import SwiftUI

struct PaperHomeView: View {
    @EnvironmentObject private var store: PaperStore
    @State private var fixtureID = L10n.fixturePlaceholder

    var body: some View {
        Group {
            if store.hasSession {
                List {
                    Section {
                        Label(L10n.simulationDisclosure, systemImage: "testtube.2")
                            .foregroundStyle(.secondary)
                    }
                    if let cash = store.portfolio?.cash {
                        Section(L10n.portfolio) {
                            CashRow(label: L10n.totalBuyingPower, value: cash.totalBuyingPower)
                            CashRow(label: L10n.settledCash, value: cash.settled)
                            CashRow(label: L10n.provisionalBuyingPower, value: cash.provisionalBuyingPower)
                        }
                    }
                    Section(L10n.orders) {
                        if store.orders.isEmpty {
                            Text(L10n.noOrders).foregroundStyle(.secondary)
                        } else {
                            ForEach(store.orders.prefix(3)) { order in
                                OrderSummary(order: order)
                            }
                        }
                    }
                }
                .refreshable { await store.refresh() }
            } else {
                Form {
                    Section {
                        VStack(alignment: .leading, spacing: 12) {
                            Image(systemName: "chart.line.uptrend.xyaxis")
                                .font(.largeTitle)
                                .foregroundStyle(.green)
                                .accessibilityHidden(true)
                            Text(L10n.registrationTitle)
                                .font(.title.bold())
                            Text(L10n.registrationMessage)
                                .foregroundStyle(.secondary)
                            Label(L10n.simulationDisclosure, systemImage: "testtube.2")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.vertical)
                    }
                    Section {
                        TextField(L10n.fixtureID, text: $fixtureID)
                            .paperFixtureInput()
                        Button(L10n.createAccount) {
                            Task { await store.register(fixtureID: fixtureID) }
                        }
                        .disabled(fixtureID.count < 3 || store.state == .loading)
                    }
                }
            }
        }
        .navigationTitle(L10n.appName)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Text(L10n.simulationBadge)
                    .font(.caption.monospaced().bold())
                    .foregroundStyle(.green)
            }
        }
    }
}

struct PaperMarketsView: View {
    @EnvironmentObject private var store: PaperStore
    @State private var query = ""
    @State private var side = "BUY"
    @State private var orderType = "MARKET"
    @State private var timeInForce = "DAY"
    @State private var quantity = "0.5"
    @State private var limitPrice = ""

    var body: some View {
        Group {
            if !store.hasSession {
                ContentUnavailableView(
                    L10n.registrationTitle,
                    systemImage: "person.badge.key",
                    description: Text(L10n.sessionRequired)
                )
            } else {
                List {
                    Section {
                        HStack {
                            TextField(L10n.searchPrompt, text: $query)
                                .paperSymbolInput()
                            Button(L10n.searchAction) {
                                Task { await store.search(query: query) }
                            }
                        }
                    }
                    Section(L10n.markets) {
                        if store.instruments.isEmpty {
                            Text(L10n.noInstruments).foregroundStyle(.secondary)
                        } else {
                            ForEach(store.instruments) { instrument in
                                Button {
                                    Task { await store.select(instrument) }
                                } label: {
                                    InstrumentRow(instrument: instrument)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                    Section(L10n.quote) {
                        if let quote = store.quote {
                            QuoteCard(quote: quote)
                        } else {
                            Text(L10n.quoteUnavailable).foregroundStyle(.secondary)
                        }
                    }
                    Section(L10n.orderTicket) {
                        Picker(L10n.side, selection: $side) {
                            Text(L10n.buy).tag("BUY")
                            Text(L10n.sell).tag("SELL")
                        }
                        .pickerStyle(.segmented)
                        Picker(L10n.orderType, selection: $orderType) {
                            Text(L10n.market).tag("MARKET")
                            Text(L10n.limit).tag("LIMIT")
                        }
                        Picker(L10n.timeInForce, selection: $timeInForce) {
                            Text(L10n.day).tag("DAY")
                            Text(L10n.gtc).tag("GTC")
                        }
                        TextField(L10n.quantity, text: $quantity)
                            .paperDecimalInput()
                        if orderType == "LIMIT" {
                            TextField(L10n.limitPrice, text: $limitPrice)
                                .paperDecimalInput()
                        }
                        if let reason = disabledReason {
                            Label(reason, systemImage: "info.circle")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Button(L10n.placeOrder) {
                            Task {
                                await store.submit(
                                    side: side,
                                    orderType: orderType,
                                    timeInForce: timeInForce,
                                    quantity: quantity,
                                    limitPrice: limitPrice
                                )
                            }
                        }
                        .disabled(disabledReason != nil || store.state == .loading)
                    }
                }
            }
        }
        .navigationTitle(L10n.markets)
    }

    private var disabledReason: String? {
        guard let instrument = store.selectedInstrument else {
            return L10n.selectInstrumentReason
        }
        guard instrument.capability.paperTradable else {
            return L10n.viewOnlyReason
        }
        guard quantity.isPositiveDecimal else {
            return L10n.quantityReason
        }
        if orderType == "LIMIT", !limitPrice.isPositiveDecimal {
            return L10n.limitPriceReason
        }
        return nil
    }
}

struct PaperPortfolioView: View {
    @EnvironmentObject private var store: PaperStore

    var body: some View {
        Group {
            if !store.hasSession {
                ContentUnavailableView(
                    L10n.registrationTitle,
                    systemImage: "person.badge.key",
                    description: Text(L10n.sessionRequired)
                )
            } else if let portfolio = store.portfolio {
                List {
                    Section(L10n.portfolio) {
                        CashRow(label: L10n.settledCash, value: portfolio.cash.settled)
                        CashRow(label: L10n.withdrawableCash, value: portfolio.cash.withdrawable)
                        CashRow(
                            label: L10n.provisionalBuyingPower,
                            value: portfolio.cash.provisionalBuyingPower,
                            provisional: true
                        )
                        CashRow(label: L10n.totalBuyingPower, value: portfolio.cash.totalBuyingPower)
                    }
                    Section(L10n.positions) {
                        if portfolio.positions.isEmpty {
                            ContentUnavailableView(
                                L10n.noPositions,
                                systemImage: "chart.bar.doc.horizontal",
                                description: Text(L10n.noPositionsMessage)
                            )
                        } else {
                            ForEach(portfolio.positions) { position in
                                VStack(alignment: .leading, spacing: 7) {
                                    HStack {
                                        Text(position.symbol).font(.headline.monospaced())
                                        Spacer()
                                        QuoteStatusLabel(status: position.quoteStatus)
                                    }
                                    LabeledContent(L10n.quantity, value: position.quantity)
                                    LabeledContent(L10n.last, value: "$\(position.marketPrice)")
                                    LabeledContent(L10n.portfolio, value: "$\(position.marketValue)")
                                }
                            }
                        }
                    }
                    Section(L10n.orders) {
                        if store.orders.isEmpty {
                            Text(L10n.noOrders).foregroundStyle(.secondary)
                        } else {
                            ForEach(store.orders) { order in
                                VStack(alignment: .leading, spacing: 10) {
                                    OrderSummary(order: order)
                                    if order.isActionable {
                                        HStack {
                                            Button(L10n.advanceReplay) {
                                                Task { await store.replay(order) }
                                            }
                                            .buttonStyle(.borderedProminent)
                                            Button(L10n.cancelOrder, role: .destructive) {
                                                Task { await store.cancel(order) }
                                            }
                                            .buttonStyle(.bordered)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                .refreshable { await store.refresh() }
            } else {
                ProgressView()
            }
        }
        .navigationTitle(L10n.portfolio)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(L10n.refresh) { Task { await store.refresh() } }
            }
        }
    }
}

struct PaperAccountView: View {
    @EnvironmentObject private var store: PaperStore

    var body: some View {
        List {
            if let account = store.account {
                Section(L10n.account) {
                    LabeledContent(L10n.fixtureID, value: account.fixtureId)
                    LabeledContent(L10n.simulationBadge, value: String(account.id.prefix(8)))
                }
                Section {
                    Button(L10n.endSession, role: .destructive) {
                        store.endSession()
                    }
                }
            } else {
                ContentUnavailableView(
                    L10n.registrationTitle,
                    systemImage: "person.badge.key",
                    description: Text(L10n.sessionRequired)
                )
            }
        }
        .navigationTitle(L10n.account)
    }
}

private struct InstrumentRow: View {
    let instrument: Instrument

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(instrument.symbol).font(.headline.monospaced())
                Text(instrument.displayName).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text(L10n.assetType(instrument.assetType)).font(.caption)
                Text(instrument.capability.paperTradable ? L10n.paperEligible : L10n.viewOnly)
                    .font(.caption2.bold())
                    .foregroundStyle(instrument.capability.paperTradable ? Color.green : Color.orange)
            }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

private struct QuoteCard: View {
    let quote: PaperQuote

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(quote.symbol).font(.title2.bold().monospaced())
                Spacer()
                QuoteStatusLabel(status: quote.status)
            }
            HStack {
                QuoteValue(label: L10n.bid, value: quote.bid)
                Spacer()
                QuoteValue(label: L10n.ask, value: quote.ask)
                Spacer()
                QuoteValue(label: L10n.last, value: quote.last)
            }
            LabeledContent(L10n.observed, value: quote.observedAt)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .contain)
    }
}

private struct QuoteValue: View {
    let label: String
    let value: String?

    var body: some View {
        VStack(alignment: .leading) {
            Text(label).font(.caption).foregroundStyle(.secondary)
            Text(value.map { "$\($0)" } ?? "—").font(.headline.monospaced())
        }
    }
}

private struct QuoteStatusLabel: View {
    let status: QuoteStatus

    var body: some View {
        Text(L10n.quoteStatus(status))
            .font(.caption2.bold().monospaced())
            .foregroundStyle(status == .simulated ? Color.green : Color.orange)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(.quaternary, in: Capsule())
    }
}

private struct CashRow: View {
    let label: String
    let value: String
    var provisional = false

    var body: some View {
        LabeledContent {
            Text("$\(value)").font(.body.monospaced())
        } label: {
            Label(label, systemImage: provisional ? "clock.arrow.circlepath" : "dollarsign.circle")
                .foregroundStyle(provisional ? Color.orange : Color.primary)
        }
    }
}

private struct OrderSummary: View {
    let order: PaperOrder

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text("\(order.side) \(order.symbol)").font(.headline.monospaced())
                Spacer()
                Text(L10n.orderStatus(order.status)).font(.caption.bold())
            }
            Text("\(order.orderType) · \(order.timeInForce)")
                .font(.caption)
                .foregroundStyle(.secondary)
            LabeledContent(L10n.filled, value: "\(order.filledQuantity) / \(order.quantity)")
                .font(.caption)
            QuoteStatusLabel(status: order.quoteStatus)
        }
        .accessibilityElement(children: .combine)
    }
}

struct PaperStatusOverlay: View {
    @EnvironmentObject private var store: PaperStore

    var body: some View {
        Group {
            switch store.state {
            case .loading:
                ProgressView().padding().background(.regularMaterial, in: Capsule())
            case let .failed(code, message):
                StatusMessage(code: code, message: message, color: .red)
            case .unauthorized:
                StatusMessage(
                    code: "AUTHENTICATION_REQUIRED",
                    message: L10n.sessionRequired,
                    color: .orange
                )
            default:
                if let notice = store.notice {
                    StatusMessage(code: L10n.simulationBadge, message: notice, color: .green)
                }
            }
        }
    }
}

private struct StatusMessage: View {
    @EnvironmentObject private var store: PaperStore
    let code: String
    let message: String
    let color: Color

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "info.circle.fill").foregroundStyle(color)
            VStack(alignment: .leading) {
                Text(code).font(.caption2.bold().monospaced())
                Text(message).font(.caption)
                Text(L10n.retryGuidance).font(.caption2).foregroundStyle(.secondary)
            }
            Button(L10n.dismiss) { store.clearMessage() }
                .buttonStyle(.borderless)
        }
        .padding()
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .shadow(radius: 8)
        .accessibilityElement(children: .combine)
    }
}

extension String {
    var isPositiveDecimal: Bool {
        guard count <= 40,
              range(of: #"^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$"#, options: .regularExpression) != nil
        else { return false }
        return range(of: #"^0(?:\.0+)?$"#, options: .regularExpression) == nil
    }
}

extension View {
    @ViewBuilder
    func paperFixtureInput() -> some View {
        #if os(iOS)
        textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        #else
        self
        #endif
    }

    @ViewBuilder
    func paperSymbolInput() -> some View {
        #if os(iOS)
        textInputAutocapitalization(.characters)
            .autocorrectionDisabled()
        #else
        self
        #endif
    }

    @ViewBuilder
    func paperDecimalInput() -> some View {
        #if os(iOS)
        keyboardType(.decimalPad)
        #else
        self
        #endif
    }
}
