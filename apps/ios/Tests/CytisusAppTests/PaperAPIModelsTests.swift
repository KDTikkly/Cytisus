import Foundation
import XCTest
@testable import CytisusApp

final class PaperAPIModelsTests: XCTestCase {
    func testDecodesExactDecimalStringsAndQuoteStatusFromSharedContract() throws {
        let payload = Data(
            #"{"instrument_id":"8f3c34b4-264e-4ccf-92fb-074b0a4b6981","symbol":"AAPL","replay_cursor":0,"bid":"189.90","ask":"190.10","last":"190.00","status":"SIMULATED","market_status":"OPEN","observed_at":"2026-07-19T14:30:00Z","provider":"local-market-simulator"}"#.utf8
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        let quote = try decoder.decode(PaperQuote.self, from: payload)

        XCTAssertEqual(quote.ask, "190.10")
        XCTAssertEqual(quote.status, .simulated)
        XCTAssertEqual(L10n.quoteStatus(quote.status), "Simulated")
    }

    func testOrderActionStateMatchesOpenAPIStateMachine() throws {
        let payload = Data(
            #"{"id":"be8cfd48-8c3c-44dd-a5e0-922b81815d00","symbol":"AAPL","side":"BUY","order_type":"MARKET","time_in_force":"DAY","quantity":"1.5","limit_price":null,"status":"PARTIALLY_FILLED","rejection_code":null,"filled_quantity":"0.75","average_fill_price":"190.1","reference_price":"190.1","quote_status":"SIMULATED","quote_observed_at":"2026-07-19T14:30:00Z","replay_cursor":0,"provider_order_id":"paper-order","deterministic_replay":true,"created_at":"2026-07-19T14:30:00Z","updated_at":"2026-07-19T14:30:00Z","fills":[]}"#.utf8
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        let order = try decoder.decode(PaperOrder.self, from: payload)

        XCTAssertTrue(order.isActionable)
        XCTAssertEqual(order.quantity, "1.5")
        XCTAssertEqual(L10n.orderStatus(order.status), "Partially filled")
    }
}
