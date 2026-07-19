import Foundation

// Generated-client-ready surface for the Crypto schemas in the shared
// OpenAPI contract. Exact financial values intentionally remain strings.

struct CryptoAssetList: Decodable, Sendable {
    let items: [CryptoAsset]
    let quoteCurrency: String
    let mode: String
}

struct CryptoAsset: Decodable, Identifiable, Hashable, Sendable {
    var id: String { symbol }

    let symbol: String
    let displayName: String
    let isStablecoin: Bool
    let tradable: Bool
    let precision: Int
    let quoteCurrency: String
    let networks: [CryptoNetwork]
}

struct CryptoNetwork: Decodable, Hashable, Sendable {
    let code: String
    let displayName: String
    let depositEnabled: Bool
    let withdrawalEnabled: Bool
    let nativeDeployment: Bool
}

struct CryptoPortfolio: Decodable, Sendable {
    let items: [CryptoBalance]
    let custodyModel: String
    let mode: String
}

struct CryptoBalance: Decodable, Identifiable, Hashable, Sendable {
    var id: String { asset }

    let asset: String
    let settled: String
    let held: String
    let frozen: String
    let custodyModel: String
}

struct CryptoConversionRequest: Encodable, Sendable {
    let sourceAsset: String
    let destinationAsset: String
    let sourceAmount: String
    let simulationScenario: String
}

struct CryptoConversionList: Decodable, Sendable {
    let items: [CryptoConversion]
    let mode: String
}

struct CryptoConversion: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let sourceAsset: String
    let destinationAsset: String
    let sourceAmount: String
    let simulationScenario: String
    let status: String
    let reasonCode: String?
    let nextAction: String
    let policyVersion: String
    let complianceCaseId: String?
    let legs: [CryptoLeg]
    let replayed: Bool
    let mode: String
    let createdAt: String
    let updatedAt: String
}

struct CryptoLeg: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let sequence: Int
    let side: String
    let asset: String
    let quoteCurrency: String
    let inputAmount: String
    let filledQuantity: String
    let referencePrice: String
    let averageExecutionPrice: String
    let grossUsd: String
    let venueFeeUsd: String
    let platformFeeUsd: String
    let finalCustomerUsd: String
    let priceImprovementUsd: String
    let status: String
    let failureCode: String?
    let ledgerTransactionId: String?
    let children: [CryptoChildOrder]
}

struct CryptoChildOrder: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let sequence: Int
    let venue: String
    let clientOrderId: String
    let providerOrderId: String?
    let requestedQuantity: String
    let filledQuantity: String
    let quoteBid: String
    let quoteAsk: String
    let venueFeeRate: String
    let effectiveUnitPrice: String
    let executionPrice: String
    let grossUsd: String
    let venueFeeUsd: String
    let status: String
    let failureCode: String?
    let quoteObservedAt: String
    let fills: [CryptoFill]
}

struct CryptoFill: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let venue: String
    let externalFillId: String
    let quantity: String
    let price: String
    let grossUsd: String
    let venueFeeUsd: String
    let occurredAt: String
}
