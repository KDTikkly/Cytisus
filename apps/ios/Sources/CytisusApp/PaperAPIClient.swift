import Foundation

enum PaperAPIError: LocalizedError, Equatable, Sendable {
    case invalidBaseURL
    case invalidResponse
    case server(code: String, message: String, status: Int)

    var errorDescription: String? {
        switch self {
        case .invalidBaseURL:
            L10n.invalidAPIURL
        case .invalidResponse:
            L10n.invalidAPIResponse
        case let .server(_, message, _):
            message
        }
    }

    var code: String {
        switch self {
        case .invalidBaseURL:
            "INVALID_API_URL"
        case .invalidResponse:
            "INVALID_API_RESPONSE"
        case let .server(code, _, _):
            code
        }
    }
}

actor PaperAPIClient {
    private let baseURL: URL
    private let session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    init(baseURL: URL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
        decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        encoder = JSONEncoder()
        encoder.keyEncodingStrategy = .convertToSnakeCase
    }

    static func configured() -> PaperAPIClient {
        let configured = ProcessInfo.processInfo.environment["CYTISUS_API_BASE_URL"]
            ?? "http://localhost:8080"
        return PaperAPIClient(baseURL: URL(string: configured)!)
    }

    func register(fixtureID: String) async throws -> PaperRegistrationResponse {
        try await send(
            path: "/v1/paper/registrations",
            method: "POST",
            body: PaperRegistrationRequest(fixtureId: fixtureID)
        )
    }

    func search(query: String) async throws -> [Instrument] {
        var components = URLComponents()
        components.path = "/v1/instruments"
        components.queryItems = [
            URLQueryItem(name: "q", value: query),
            URLQueryItem(name: "page_size", value: "20"),
        ]
        let list: InstrumentList = try await send(path: components.string ?? "/v1/instruments")
        return list.items
    }

    func quote(symbol: String) async throws -> PaperQuote {
        try await send(path: "/v1/market-data/quotes/\(symbol)")
    }

    func portfolio(accessToken: String) async throws -> PaperPortfolio {
        try await send(path: "/v1/portfolio", accessToken: accessToken)
    }

    func orders(accessToken: String) async throws -> [PaperOrder] {
        let list: PaperOrderList = try await send(
            path: "/v1/orders?page_size=50",
            accessToken: accessToken
        )
        return list.items
    }

    func submit(
        accessToken: String,
        request: SubmitOrderRequest,
        idempotencyKey: String
    ) async throws -> PaperOrder {
        try await send(
            path: "/v1/orders",
            method: "POST",
            accessToken: accessToken,
            idempotencyKey: idempotencyKey,
            body: request
        )
    }

    func replay(accessToken: String, orderID: String, idempotencyKey: String) async throws -> PaperOrder {
        try await send(
            path: "/v1/orders/\(orderID)/replay",
            method: "POST",
            accessToken: accessToken,
            idempotencyKey: idempotencyKey
        )
    }

    func cancel(accessToken: String, orderID: String, idempotencyKey: String) async throws -> PaperOrder {
        try await send(
            path: "/v1/orders/\(orderID)/cancel",
            method: "POST",
            accessToken: accessToken,
            idempotencyKey: idempotencyKey
        )
    }

    private func send<Response: Decodable>(
        path: String,
        method: String = "GET",
        accessToken: String? = nil,
        idempotencyKey: String? = nil
    ) async throws -> Response {
        try await send(
            path: path,
            method: method,
            accessToken: accessToken,
            idempotencyKey: idempotencyKey,
            encodedBody: nil
        )
    }

    private func send<Response: Decodable, Body: Encodable>(
        path: String,
        method: String,
        accessToken: String? = nil,
        idempotencyKey: String? = nil,
        body: Body
    ) async throws -> Response {
        try await send(
            path: path,
            method: method,
            accessToken: accessToken,
            idempotencyKey: idempotencyKey,
            encodedBody: try encoder.encode(body)
        )
    }

    private func send<Response: Decodable>(
        path: String,
        method: String,
        accessToken: String?,
        idempotencyKey: String?,
        encodedBody: Data?
    ) async throws -> Response {
        guard let url = URL(string: path, relativeTo: baseURL) else {
            throw PaperAPIError.invalidBaseURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = encodedBody
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if encodedBody != nil {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let accessToken {
            request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        }
        if let idempotencyKey {
            request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        }

        let (data, response) = try await session.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw PaperAPIError.invalidResponse
        }
        guard (200 ... 299).contains(httpResponse.statusCode) else {
            if let envelope = try? decoder.decode(APIErrorEnvelope.self, from: data) {
                throw PaperAPIError.server(
                    code: envelope.error.code,
                    message: envelope.error.message,
                    status: httpResponse.statusCode
                )
            }
            throw PaperAPIError.invalidResponse
        }
        return try decoder.decode(Response.self, from: data)
    }
}
