import XCTest
@testable import CytisusApp

final class L10nTests: XCTestCase {
    func testLockedNavigationLabels() {
        XCTAssertEqual(
            [L10n.home, L10n.markets, L10n.portfolio, L10n.card, L10n.account],
            ["Home", "Markets", "Portfolio", "Card", "Account"]
        )
    }

    func testFoundationIsExplicitlySimulated() {
        XCTAssertEqual(L10n.simulationBadge, "SIMULATED")
    }
}
