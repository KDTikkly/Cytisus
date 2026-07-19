import Foundation

// Generated-client-ready Card surface matching contracts/openapi/openapi.yaml.
// Financial values remain exact decimal strings and are never decoded as Double.

struct CardProfile: Decodable, Sendable {
    let customerReference: String
    let repaymentMode: String
    let spendingStatus: String
    let spendingStatusReason: String?
    let policyVersion: String
    let cards: [CardRecord]
    let mode: String
}

struct CardRecord: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let cardType: String
    let status: String
    let displayName: String
    let last4: String
    let pinSet: Bool
    let appleWalletStatus: String
    let googleWalletStatus: String
    let replayed: Bool
    let createdAt: String
    let updatedAt: String
}

struct CardSpendingPower: Decodable, Sendable {
    let cashEligibleUsd: String
    let collateralEligibleUsd: String
    let grossUsd: String
    let outstandingHoldsUsd: String
    let receivableUsd: String
    let availableUsd: String
    let repaymentMode: String
    let spendingStatus: String
    let primaryExplanation: String
    let drivers: [CardCollateralDriver]
    let calculatedAt: String
    let mode: String
}

struct CardCollateralDriver: Decodable, Identifiable, Hashable, Sendable {
    var id: String { symbol }

    let symbol: String
    let quantity: String
    let referencePriceUsd: String
    let eligibleValueUsd: String
    let quoteStatus: String
    let marketStatus: String
    let eligible: Bool
    let exclusionReason: String?
}

struct CardAuthorizationList: Decodable, Sendable {
    let items: [CardAuthorization]
    let mode: String
}

struct CardAuthorization: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let cardId: String
    let merchantName: String
    let merchantAmount: String
    let merchantCurrency: String
    let authorizedUsd: String
    let status: String
    let offline: Bool
    let entryMode: String
    let declineCode: String?
    let replayed: Bool
    let occurredAt: String
}

struct CardCaptureList: Decodable, Sendable {
    let items: [CardCapture]
    let mode: String
}

struct CardCapture: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let authorizationId: String
    let merchantAmount: String
    let merchantCurrency: String
    let clearingFxRate: String
    let settledUsd: String
    let tipUsd: String
    let cashRepaidUsd: String
    let autoSellRepaidUsd: String
    let refundedUsd: String
    let status: String
    let replayed: Bool
    let occurredAt: String
}

struct CardDisputeList: Decodable, Sendable {
    let items: [CardDispute]
}

struct CardDispute: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let captureId: String
    let amountUsd: String
    let reasonCode: String
    let status: String
    let outcome: String?
    let complianceCaseId: String
    let openedAt: String
    let resolvedAt: String?
}

struct CardStatementList: Decodable, Sendable {
    let items: [CardStatement]
}

struct CardStatement: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let periodStart: String
    let periodEnd: String
    let amountDueUsd: String
    let status: String
    let dueAt: String
    let paidAt: String?
}

struct CardNotificationList: Decodable, Sendable {
    let items: [CardNotification]
}

struct CardNotification: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let eventType: String
    let titleKey: String
    let bodyKey: String
    let deliveryStatus: String
    let createdAt: String
}

struct CardCreateRequest: Encodable, Sendable {
    let cardType: String
}

struct CardActionRequest: Encodable, Sendable {
    let action: String
    let reasonCode: String
}

struct CardRepaymentRequest: Encodable, Sendable {
    let repaymentMode: String
}

struct CardAuthorizationRequest: Encodable, Sendable {
    let externalEventId: String
    let cardId: String
    let merchantName: String
    let merchantCategoryCode: String
    let merchantAmount: String
    let merchantCurrency: String
    let entryMode: String
    let offline: Bool
    let simulationScenario: String
}

struct CardCaptureRequest: Encodable, Sendable {
    let externalEventId: String
    let authorizationId: String
    let merchantAmount: String
    let merchantCurrency: String
    let final: Bool
    let simulationScenario: String
}
