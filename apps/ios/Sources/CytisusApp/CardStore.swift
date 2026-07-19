import Combine
import Foundation

@MainActor
final class CardStore: ObservableObject {
    enum State: Equatable {
        case idle
        case loading
        case loaded
        case failed(code: String, message: String)
    }

    @Published private(set) var profile: CardProfile?
    @Published private(set) var spendingPower: CardSpendingPower?
    @Published private(set) var authorizations: [CardAuthorization] = []
    @Published private(set) var captures: [CardCapture] = []
    @Published private(set) var disputes: [CardDispute] = []
    @Published private(set) var statements: [CardStatement] = []
    @Published private(set) var notifications: [CardNotification] = []
    @Published private(set) var state: State = .idle
    @Published private(set) var notice: String?

    private let client: PaperAPIClient

    init(client: PaperAPIClient = .configured()) {
        self.client = client
    }

    func load(accessToken: String) async {
        await perform(accessToken: accessToken) {}
    }

    func create(accessToken: String, cardType: String) async {
        await perform(accessToken: accessToken) {
            _ = try await client.createCard(
                accessToken: accessToken,
                cardType: cardType,
                idempotencyKey: "ios-card-create.\(UUID().uuidString)"
            )
            notice = L10n.cardCreated
        }
    }

    func action(accessToken: String, cardID: String, action: String) async {
        await perform(accessToken: accessToken) {
            _ = try await client.actOnCard(
                accessToken: accessToken,
                cardID: cardID,
                action: action,
                idempotencyKey: "ios-card-action.\(UUID().uuidString)"
            )
            notice = L10n.cardStateUpdated
        }
    }

    func configureRepayment(accessToken: String, mode: String) async {
        await perform(accessToken: accessToken) {
            _ = try await client.configureCardRepayment(
                accessToken: accessToken,
                repaymentMode: mode,
                idempotencyKey: "ios-card-repayment.\(UUID().uuidString)"
            )
            notice = L10n.cardRepaymentUpdated
        }
    }

    func authorize(
        accessToken: String,
        cardID: String,
        amount: String,
        currency: String,
        offline: Bool,
        scenario: String
    ) async {
        await perform(accessToken: accessToken) {
            _ = try await client.simulateCardAuthorization(
                accessToken: accessToken,
                request: CardAuthorizationRequest(
                    externalEventId: "ios-terminal-auth.\(UUID().uuidString)",
                    cardId: cardID,
                    merchantName: "Cytisus iOS Terminal",
                    merchantCategoryCode: "5812",
                    merchantAmount: amount,
                    merchantCurrency: currency,
                    entryMode: offline ? "OFFLINE_SIMULATOR" : "NFC_SIMULATOR",
                    offline: offline,
                    simulationScenario: scenario
                )
            )
            notice = L10n.cardAuthorizationProcessed
        }
    }

    func capture(
        accessToken: String,
        authorizationID: String,
        amount: String,
        currency: String,
        final: Bool,
        scenario: String
    ) async {
        await perform(accessToken: accessToken) {
            _ = try await client.simulateCardCapture(
                accessToken: accessToken,
                request: CardCaptureRequest(
                    externalEventId: "ios-terminal-capture.\(UUID().uuidString)",
                    authorizationId: authorizationID,
                    merchantAmount: amount,
                    merchantCurrency: currency,
                    final: final,
                    simulationScenario: scenario
                )
            )
            notice = L10n.cardCaptureProcessed
        }
    }

    func clearMessage() {
        notice = nil
        if case .failed = state { state = .loaded }
    }

    private func perform(
        accessToken: String,
        operation: () async throws -> Void
    ) async {
        state = .loading
        notice = nil
        do {
            try await operation()
            try await refresh(accessToken: accessToken)
            state = .loaded
        } catch let error as PaperAPIError {
            state = .failed(code: error.code, message: error.errorDescription ?? L10n.unknownError)
        } catch {
            state = .failed(code: "NETWORK_ERROR", message: L10n.networkError)
        }
    }

    private func refresh(accessToken: String) async throws {
        async let nextProfile = client.cardProfile(accessToken: accessToken)
        async let nextPower = client.cardSpendingPower(accessToken: accessToken)
        async let nextAuthorizations = client.cardAuthorizations(accessToken: accessToken)
        async let nextCaptures = client.cardCaptures(accessToken: accessToken)
        async let nextDisputes = client.cardDisputes(accessToken: accessToken)
        async let nextStatements = client.cardStatements(accessToken: accessToken)
        async let nextNotifications = client.cardNotifications(accessToken: accessToken)
        profile = try await nextProfile
        spendingPower = try await nextPower
        authorizations = try await nextAuthorizations
        captures = try await nextCaptures
        disputes = try await nextDisputes
        statements = try await nextStatements
        notifications = try await nextNotifications
    }
}
