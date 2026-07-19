import Foundation
import Testing
@testable import CytisusApp

struct CardAPIModelsTests {
    @Test func decodesExactDecimalStringsAndTruthfulWalletState() throws {
        let data = Data(
            #"{"customer_reference":"paper-card","repayment_mode":"CASH_ONLY","spending_status":"ACTIVE","spending_status_reason":null,"policy_version":"card-sim-v1","cards":[{"id":"d55c0ec3-7526-4a04-a2c1-97fa77600b28","card_type":"VIRTUAL","status":"ACTIVE","display_name":"Virtual card","last4":"4242","pin_set":false,"apple_wallet_status":"UNAVAILABLE_SIMULATOR","google_wallet_status":"UNAVAILABLE_SIMULATOR","replayed":false,"created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z"}],"mode":"SIMULATED"}"#.utf8
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        let profile = try decoder.decode(CardProfile.self, from: data)

        #expect(profile.mode == "SIMULATED")
        #expect(profile.cards.first?.appleWalletStatus == "UNAVAILABLE_SIMULATOR")
        #expect(profile.cards.first?.last4 == "4242")
    }

    @Test func decodesSpendingPowerWithoutFloatingPoint() throws {
        let data = Data(
            #"{"cash_eligible_usd":"1000.123456789012345678","collateral_eligible_usd":"0","gross_usd":"1000.123456789012345678","outstanding_holds_usd":"25","receivable_usd":"0","available_usd":"975.123456789012345678","repayment_mode":"CASH_ONLY","spending_status":"ACTIVE","primary_explanation":"Eligible settled cash is the primary spending-power driver.","drivers":[],"calculated_at":"2026-07-19T00:00:00Z","mode":"SIMULATED"}"#.utf8
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        let power = try decoder.decode(CardSpendingPower.self, from: data)

        #expect(power.availableUsd == "975.123456789012345678")
        #expect(power.cashEligibleUsd == "1000.123456789012345678")
    }
}
