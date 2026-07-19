import Foundation
import XCTest
@testable import CytisusApp

final class CryptoAPIModelsTests: XCTestCase {
    func testDecodesTwoExplicitUSDLegsAndExactFees() throws {
        let payload = Data(
            #"{"id":"be8cfd48-8c3c-44dd-a5e0-922b81815d00","source_asset":"BTC","destination_asset":"ETH","source_amount":"0.5","simulation_scenario":"NORMAL","status":"FILLED","reason_code":null,"next_action":"Complete.","policy_version":"crypto-sim-v1","compliance_case_id":null,"legs":[{"id":"0194265e-b908-77aa-b975-6f8d6f294ce4","sequence":1,"side":"SELL","asset":"BTC","quote_currency":"USD","input_amount":"0.5","filled_quantity":"0.5","reference_price":"68405.5","average_execution_price":"68410.5","gross_usd":"34205.25","venue_fee_usd":"17.102625","platform_fee_usd":"68.4105","final_customer_usd":"34119.736875","price_improvement_usd":"2.5","status":"FILLED","failure_code":null,"ledger_transaction_id":"8f3c34b4-264e-4ccf-92fb-074b0a4b6981","children":[]},{"id":"0194265e-b908-77aa-b975-6f8d6f294ce5","sequence":2,"side":"BUY","asset":"ETH","quote_currency":"USD","input_amount":"34119.736875","filled_quantity":"9.49","reference_price":"3589.05","average_execution_price":"3588.9","gross_usd":"34059.661","venue_fee_usd":"17.0298305","platform_fee_usd":"68.119322","final_customer_usd":"34144.8101525","price_improvement_usd":"1.4235","status":"FILLED","failure_code":null,"ledger_transaction_id":"8f3c34b4-264e-4ccf-92fb-074b0a4b6982","children":[]}],"replayed":false,"mode":"SIMULATED","created_at":"2026-07-19T14:30:00Z","updated_at":"2026-07-19T14:30:01Z"}"#.utf8
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        let conversion = try decoder.decode(CryptoConversion.self, from: payload)

        XCTAssertEqual(conversion.legs.count, 2)
        XCTAssertEqual(conversion.legs.map(\.quoteCurrency), ["USD", "USD"])
        XCTAssertEqual(conversion.legs[0].venueFeeUsd, "17.102625")
        XCTAssertEqual(conversion.legs[1].platformFeeUsd, "68.119322")
    }

    func testStablecoinFlagDoesNotEncodeFixedPrice() throws {
        let payload = Data(
            #"{"items":[{"symbol":"USDC","display_name":"USD Coin","is_stablecoin":true,"tradable":true,"precision":18,"quote_currency":"USD","networks":[{"code":"BASE","display_name":"Base","deposit_enabled":true,"withdrawal_enabled":true,"native_deployment":true}]}],"quote_currency":"USD","mode":"SIMULATED"}"#.utf8
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        let assets = try decoder.decode(CryptoAssetList.self, from: payload)

        XCTAssertTrue(assets.items[0].isStablecoin)
        XCTAssertEqual(assets.quoteCurrency, "USD")
    }
}
