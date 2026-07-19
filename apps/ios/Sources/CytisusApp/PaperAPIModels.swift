import Foundation

// Contract surface mirrors contracts/openapi/openapi.yaml. Financial values
// remain exact decimal strings across decoding, presentation, and commands.

struct PaperRegistrationRequest: Encodable, Sendable {
    let fixtureId: String
}

struct PaperRegistrationResponse: Decodable, Sendable {
    let account: PaperAccount
    let accessToken: String
    let mode: String
    let replayed: Bool
}

struct PaperAccount: Decodable, Sendable {
    let id: String
    let fixtureId: String
    let customerReference: String
    let initialCash: String
    let createdAt: String
}

struct InstrumentList: Decodable, Sendable {
    let items: [Instrument]
}

struct Instrument: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let symbol: String
    let displayName: String
    let assetType: String
    let primaryExchange: String
    let currency: String
    let listed: Bool
    let capability: InstrumentCapability
}

struct InstrumentCapability: Decodable, Hashable, Sendable {
    let searchable: Bool
    let quoteEnabled: Bool
    let paperTradable: Bool
    let liveTradable: Bool
    let fractionalEnabled: Bool
    let transferOutEnabled: Bool
    let rwaMintEnabled: Bool
    let userEligible: Bool
    let disabledReason: String?
    let policyVersion: String
}

enum QuoteStatus: String, Decodable, Hashable, Sendable {
    case realTime = "REAL_TIME"
    case delayed = "DELAYED"
    case indicative = "INDICATIVE"
    case simulated = "SIMULATED"
    case stale = "STALE"
    case unavailable = "UNAVAILABLE"
}

struct PaperQuote: Decodable, Hashable, Sendable {
    let instrumentId: String
    let symbol: String
    let replayCursor: Int
    let bid: String?
    let ask: String?
    let last: String?
    let status: QuoteStatus
    let marketStatus: String
    let observedAt: String
    let provider: String
}

struct SubmitOrderRequest: Encodable, Sendable {
    let symbol: String
    let side: String
    let orderType: String
    let timeInForce: String
    let quantity: String
    let limitPrice: String?
}

struct PaperFill: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let externalEventId: String
    let sequence: Int
    let quantity: String
    let price: String
    let consideration: String
    let occurredAt: String
}

struct PaperOrder: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let symbol: String
    let side: String
    let orderType: String
    let timeInForce: String
    let quantity: String
    let limitPrice: String?
    let status: String
    let rejectionCode: String?
    let filledQuantity: String
    let averageFillPrice: String?
    let referencePrice: String
    let quoteStatus: QuoteStatus
    let quoteObservedAt: String
    let replayCursor: Int
    let providerOrderId: String
    let deterministicReplay: Bool
    let createdAt: String
    let updatedAt: String
    let fills: [PaperFill]

    var isActionable: Bool {
        status == "OPEN" || status == "PARTIALLY_FILLED"
    }
}

struct PaperOrderList: Decodable, Sendable {
    let items: [PaperOrder]
}

struct PaperPosition: Decodable, Identifiable, Hashable, Sendable {
    var id: String { symbol }

    let symbol: String
    let quantity: String
    let averageCost: String
    let costBasis: String
    let realizedPnl: String
    let marketPrice: String
    let marketValue: String
    let quoteStatus: QuoteStatus
    let updatedAt: String
}

struct PaperCashSummary: Decodable, Hashable, Sendable {
    let settled: String
    let withdrawable: String
    let provisionalBuyingPower: String
    let totalBuyingPower: String
}

struct PaperPortfolio: Decodable, Hashable, Sendable {
    let cash: PaperCashSummary
    let positions: [PaperPosition]
}

struct APIErrorEnvelope: Decodable, Sendable {
    let error: APIErrorBody
}

struct APIErrorBody: Decodable, Sendable {
    let code: String
    let message: String
}
