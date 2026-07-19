import SwiftUI

struct CardRootView: View {
    @EnvironmentObject private var paperStore: PaperStore
    @StateObject private var cardStore = CardStore()
    @State private var cardType = "VIRTUAL"
    @State private var repaymentMode = "CASH_ONLY"
    @State private var selectedCardID = ""
    @State private var amount = "25"
    @State private var currency = "USD"
    @State private var scenario = "NORMAL"
    @State private var offline = false
    @State private var selectedAuthorizationID = ""
    @State private var finalCapture = true

    var body: some View {
        Group {
            if let accessToken = paperStore.accessToken {
                List {
                    Section {
                        Label(L10n.cardDisclosure, systemImage: "testtube.2")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    spendingPowerSection
                    lifecycleSection(accessToken: accessToken)
                    repaymentSection(accessToken: accessToken)
                    terminalSection(accessToken: accessToken)
                    activitySection
                    supportSection
                }
                .refreshable { await cardStore.load(accessToken: accessToken) }
                .task(id: accessToken) { await cardStore.load(accessToken: accessToken) }
            } else {
                ContentUnavailableView(
                    L10n.registrationTitle,
                    systemImage: "person.badge.key",
                    description: Text(L10n.sessionRequired)
                )
            }
        }
        .navigationTitle(L10n.card)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Text(L10n.simulationBadge)
                    .font(.caption.monospaced().bold())
                    .foregroundStyle(.green)
            }
        }
        .overlay(alignment: .bottom) {
            if let notice = cardStore.notice {
                Text(notice)
                    .font(.caption)
                    .padding(10)
                    .background(.regularMaterial, in: Capsule())
                    .onTapGesture { cardStore.clearMessage() }
                    .padding()
            }
        }
    }

    @ViewBuilder
    private var spendingPowerSection: some View {
        Section(L10n.cardSpendingPower) {
            if let power = cardStore.spendingPower {
                LabeledContent(L10n.cardAvailable, value: "$\(power.availableUsd)")
                LabeledContent(L10n.cardEligibleCash, value: "$\(power.cashEligibleUsd)")
                LabeledContent(L10n.cardEligibleCollateral, value: "$\(power.collateralEligibleUsd)")
                LabeledContent(L10n.cardHolds, value: "$\(power.outstandingHoldsUsd)")
                LabeledContent(L10n.cardReceivable, value: "$\(power.receivableUsd)")
                Text(power.primaryExplanation).font(.caption).foregroundStyle(.secondary)
            } else if cardStore.state == .loading {
                ProgressView()
            }
            if case let .failed(code, message) = cardStore.state {
                VStack(alignment: .leading, spacing: 6) {
                    Text(code).font(.caption.monospaced().bold()).foregroundStyle(.red)
                    Text(message)
                    Text(L10n.retryGuidance).font(.caption).foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    @ViewBuilder
    private func lifecycleSection(accessToken: String) -> some View {
        Section(L10n.cardDescription) {
            Picker(L10n.cardType, selection: $cardType) {
                Text(L10n.cardVirtual).tag("VIRTUAL")
                Text(L10n.cardPlastic).tag("PLASTIC")
                Text(L10n.cardMetal).tag("METAL")
            }
            Button(L10n.cardCreate) {
                Task { await cardStore.create(accessToken: accessToken, cardType: cardType) }
            }
            .disabled(cardStore.state == .loading)
            if cardStore.profile?.cards.isEmpty != false {
                Text(L10n.cardNoCards).foregroundStyle(.secondary)
            } else {
                ForEach(cardStore.profile?.cards ?? []) { card in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text("\(card.displayName) •••• \(card.last4)").font(.headline)
                            Spacer()
                            CardStatusLabel(value: card.status)
                        }
                        Text("\(L10n.cardWallet): \(card.appleWalletStatus) / \(card.googleWalletStatus)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        HStack {
                            if card.status == "CREATED" {
                                Button(L10n.cardActivate) { Task { await cardStore.action(accessToken: accessToken, cardID: card.id, action: "ACTIVATE") } }
                            } else if card.status == "ACTIVE" {
                                Button(L10n.cardFreeze) { Task { await cardStore.action(accessToken: accessToken, cardID: card.id, action: "FREEZE") } }
                            } else if card.status == "FROZEN" {
                                Button(L10n.cardUnfreeze) { Task { await cardStore.action(accessToken: accessToken, cardID: card.id, action: "UNFREEZE") } }
                            }
                        }
                        .buttonStyle(.bordered)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func repaymentSection(accessToken: String) -> some View {
        Section(L10n.cardRepayment) {
            Picker(L10n.cardRepayment, selection: $repaymentMode) {
                Text("CASH_ONLY").tag("CASH_ONLY")
                Text("CASH_THEN_AUTO_SELL").tag("CASH_THEN_AUTO_SELL")
                Text("MONTHLY_STATEMENT").tag("MONTHLY_STATEMENT")
            }
            Button(L10n.cardSaveRepayment) {
                Task { await cardStore.configureRepayment(accessToken: accessToken, mode: repaymentMode) }
            }
            Text(L10n.cardProtectedSell).font(.caption).foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func terminalSection(accessToken: String) -> some View {
        let activeCards = cardStore.profile?.cards.filter { $0.status == "ACTIVE" } ?? []
        Section(L10n.cardTerminal) {
            Text(L10n.cardTerminalDescription).font(.caption).foregroundStyle(.secondary)
            Picker(L10n.cardSelect, selection: $selectedCardID) {
                Text(L10n.cardSelect).tag("")
                ForEach(activeCards) { card in Text("•••• \(card.last4)").tag(card.id) }
            }
            TextField(L10n.cardAmount, text: $amount).paperDecimalInput()
            Picker(L10n.cardCurrency, selection: $currency) {
                ForEach(["USD", "EUR", "GBP", "JPY", "CAD"], id: \.self) { Text($0).tag($0) }
            }
            Picker(L10n.cardScenario, selection: $scenario) {
                ForEach(["NORMAL", "DECLINE", "PARTIAL", "TIMEOUT", "STALE", "VENUE_FAILURE", "PROTECTION_BREACH"], id: \.self) { Text($0).tag($0) }
            }
            Toggle(L10n.cardOffline, isOn: $offline)
            if activeCards.isEmpty {
                Label(L10n.cardNoActive, systemImage: "info.circle")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
            Button(L10n.cardAuthorize) {
                Task {
                    await cardStore.authorize(
                        accessToken: accessToken,
                        cardID: selectedCardID,
                        amount: amount,
                        currency: currency,
                        offline: offline,
                        scenario: scenario
                    )
                }
            }
            .disabled(selectedCardID.isEmpty || !amount.isPositiveDecimal || cardStore.state == .loading)

            Picker(L10n.cardAuthorization, selection: $selectedAuthorizationID) {
                Text(L10n.cardAuthorization).tag("")
                ForEach(cardStore.authorizations.filter { ["APPROVED", "PARTIALLY_CAPTURED"].contains($0.status) }) { authorization in
                    Text("\(authorization.merchantName) · $\(authorization.authorizedUsd)").tag(authorization.id)
                }
            }
            Toggle(L10n.cardFinalCapture, isOn: $finalCapture)
            Button(L10n.cardCapture) {
                Task {
                    await cardStore.capture(
                        accessToken: accessToken,
                        authorizationID: selectedAuthorizationID,
                        amount: amount,
                        currency: currency,
                        final: finalCapture,
                        scenario: scenario
                    )
                }
            }
            .disabled(selectedAuthorizationID.isEmpty || !amount.isPositiveDecimal || cardStore.state == .loading)
        }
    }

    @ViewBuilder
    private var activitySection: some View {
        Section(L10n.cardActivity) {
            if cardStore.authorizations.isEmpty, cardStore.captures.isEmpty {
                Text(L10n.cardNoActivity).foregroundStyle(.secondary)
            }
            ForEach(cardStore.authorizations.prefix(10)) { authorization in
                VStack(alignment: .leading, spacing: 6) {
                    HStack { Text(authorization.merchantName); Spacer(); CardStatusLabel(value: authorization.status) }
                    Text("\(authorization.entryMode) · \(authorization.merchantAmount) \(authorization.merchantCurrency) · $\(authorization.authorizedUsd)")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            ForEach(cardStore.captures.prefix(10)) { capture in
                VStack(alignment: .leading, spacing: 6) {
                    HStack { Text("\(L10n.cardCapture) $\(capture.settledUsd)"); Spacer(); CardStatusLabel(value: capture.status) }
                    Text("FX \(capture.clearingFxRate) · Auto-Sell $\(capture.autoSellRepaidUsd)")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    @ViewBuilder
    private var supportSection: some View {
        Section(L10n.cardDisputes) {
            if cardStore.disputes.isEmpty { Text(L10n.cardNoDisputes).foregroundStyle(.secondary) }
            ForEach(cardStore.disputes) { dispute in
                LabeledContent("$\(dispute.amountUsd)", value: dispute.outcome ?? dispute.status)
            }
        }
        Section(L10n.cardStatements) {
            if cardStore.statements.isEmpty { Text(L10n.cardNoStatements).foregroundStyle(.secondary) }
            ForEach(cardStore.statements) { statement in
                LabeledContent("\(statement.periodStart) – \(statement.periodEnd)", value: "$\(statement.amountDueUsd) · \(statement.status)")
            }
        }
        Section(L10n.cardNotifications) {
            if cardStore.notifications.isEmpty { Text(L10n.cardNoNotifications).foregroundStyle(.secondary) }
            ForEach(cardStore.notifications.prefix(10)) { notification in
                LabeledContent(notification.eventType, value: notification.deliveryStatus)
            }
        }
    }
}

private struct CardStatusLabel: View {
    let value: String

    var body: some View {
        Text(value.replacingOccurrences(of: "_", with: " "))
            .font(.caption2.bold().monospaced())
            .foregroundStyle(["ACTIVE", "APPROVED", "CAPTURED", "PAID"].contains(value) ? Color.green : Color.orange)
            .accessibilityLabel(value.replacingOccurrences(of: "_", with: " "))
    }
}
