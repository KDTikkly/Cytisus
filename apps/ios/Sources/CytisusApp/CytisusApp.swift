import SwiftUI

@main
struct CytisusApp: App {
    @StateObject private var store = PaperStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(store)
        }
    }
}

struct RootView: View {
    @EnvironmentObject private var store: PaperStore

    var body: some View {
        TabView {
            NavigationStack { PaperHomeView() }
                .tabItem { Label(L10n.home, systemImage: "house") }
            NavigationStack { PaperMarketsView() }
                .tabItem { Label(L10n.markets, systemImage: "chart.xyaxis.line") }
            NavigationStack { PaperPortfolioView() }
                .tabItem { Label(L10n.portfolio, systemImage: "briefcase") }
            NavigationStack {
                DisabledFeatureView(symbol: "creditcard")
                    .navigationTitle(L10n.card)
            }
            .tabItem { Label(L10n.card, systemImage: "creditcard") }
            NavigationStack { PaperAccountView() }
                .tabItem { Label(L10n.account, systemImage: "person.crop.circle") }
        }
        .overlay(alignment: .top) {
            PaperStatusOverlay()
                .padding()
        }
        .tint(.green)
    }
}

private struct DisabledFeatureView: View {
    let symbol: String

    var body: some View {
        ContentUnavailableView(
            L10n.featureUnavailable,
            systemImage: symbol,
            description: Text(L10n.featureUnavailableMessage)
        )
    }
}
