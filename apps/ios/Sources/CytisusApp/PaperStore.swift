import Combine
import Foundation

@MainActor
final class PaperStore: ObservableObject {
    enum LoadState: Equatable {
        case idle
        case loading
        case loaded
        case empty
        case failed(code: String, message: String)
        case unauthorized
    }

    @Published private(set) var accessToken: String?
    @Published private(set) var account: PaperAccount?
    @Published private(set) var instruments: [Instrument] = []
    @Published private(set) var selectedInstrument: Instrument?
    @Published private(set) var quote: PaperQuote?
    @Published private(set) var portfolio: PaperPortfolio?
    @Published private(set) var orders: [PaperOrder] = []
    @Published private(set) var state: LoadState = .idle
    @Published private(set) var notice: String?

    private let client: PaperAPIClient

    init(client: PaperAPIClient = .configured()) {
        self.client = client
    }

    var hasSession: Bool { accessToken != nil }

    func register(fixtureID: String) async {
        await perform {
            let registration = try await client.register(fixtureID: fixtureID)
            accessToken = registration.accessToken
            account = registration.account
            instruments = try await client.search(query: "")
            try await refreshAuthenticatedData()
        }
    }

    func search(query: String) async {
        await perform {
            instruments = try await client.search(query: query)
            if instruments.isEmpty {
                state = .empty
            }
        }
    }

    func select(_ instrument: Instrument) async {
        selectedInstrument = instrument
        await perform {
            quote = try await client.quote(symbol: instrument.symbol)
        }
    }

    func submit(
        side: String,
        orderType: String,
        timeInForce: String,
        quantity: String,
        limitPrice: String?
    ) async {
        guard let accessToken, let selectedInstrument else {
            state = .unauthorized
            return
        }
        await perform {
            _ = try await client.submit(
                accessToken: accessToken,
                request: SubmitOrderRequest(
                    symbol: selectedInstrument.symbol,
                    side: side,
                    orderType: orderType,
                    timeInForce: timeInForce,
                    quantity: quantity,
                    limitPrice: orderType == "LIMIT" ? limitPrice : nil
                ),
                idempotencyKey: "ios-order.\(UUID().uuidString)"
            )
            try await refreshAuthenticatedData()
            notice = L10n.orderAccepted
        }
    }

    func replay(_ order: PaperOrder) async {
        guard let accessToken else {
            state = .unauthorized
            return
        }
        await perform {
            _ = try await client.replay(
                accessToken: accessToken,
                orderID: order.id,
                idempotencyKey: "ios-replay.\(UUID().uuidString)"
            )
            try await refreshAuthenticatedData()
            notice = L10n.replayApplied
        }
    }

    func cancel(_ order: PaperOrder) async {
        guard let accessToken else {
            state = .unauthorized
            return
        }
        await perform {
            _ = try await client.cancel(
                accessToken: accessToken,
                orderID: order.id,
                idempotencyKey: "ios-cancel.\(UUID().uuidString)"
            )
            try await refreshAuthenticatedData()
            notice = L10n.orderCancelled
        }
    }

    func refresh() async {
        guard accessToken != nil else {
            state = .unauthorized
            return
        }
        await perform {
            try await refreshAuthenticatedData()
        }
    }

    func endSession() {
        clearSessionData()
        state = .unauthorized
    }

    func clearMessage() {
        notice = nil
        switch state {
        case .failed, .unauthorized:
            state = .idle
        default:
            break
        }
    }

    private func clearSessionData() {
        accessToken = nil
        account = nil
        selectedInstrument = nil
        quote = nil
        portfolio = nil
        orders = []
    }

    private func refreshAuthenticatedData() async throws {
        guard let accessToken else {
            throw PaperAPIError.server(
                code: "AUTHENTICATION_REQUIRED",
                message: L10n.sessionRequired,
                status: 401
            )
        }
        async let nextPortfolio = client.portfolio(accessToken: accessToken)
        async let nextOrders = client.orders(accessToken: accessToken)
        portfolio = try await nextPortfolio
        orders = try await nextOrders
    }

    private func perform(_ operation: () async throws -> Void) async {
        state = .loading
        notice = nil
        do {
            try await operation()
            if state != .empty {
                state = .loaded
            }
        } catch let error as PaperAPIError {
            if error.code == "AUTHENTICATION_REQUIRED" {
                clearSessionData()
                state = .unauthorized
            } else {
                state = .failed(
                    code: error.code,
                    message: error.errorDescription ?? L10n.unknownError
                )
            }
        } catch {
            state = .failed(code: "NETWORK_ERROR", message: L10n.networkError)
        }
    }
}
