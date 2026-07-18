import SwiftUI

@main
struct CytisusApp: App {
    var body: some Scene {
        WindowGroup {
            RootView()
        }
    }
}

struct RootView: View {
    var body: some View {
        TabView {
            PlaceholderView(title: L10n.home, symbol: "house")
                .tabItem { Label(L10n.home, systemImage: "house") }
            PlaceholderView(title: L10n.markets, symbol: "chart.xyaxis.line")
                .tabItem { Label(L10n.markets, systemImage: "chart.xyaxis.line") }
            PlaceholderView(title: L10n.portfolio, symbol: "briefcase")
                .tabItem { Label(L10n.portfolio, systemImage: "briefcase") }
            PlaceholderView(title: L10n.card, symbol: "creditcard")
                .tabItem { Label(L10n.card, systemImage: "creditcard") }
            PlaceholderView(title: L10n.account, symbol: "person.crop.circle")
                .tabItem { Label(L10n.account, systemImage: "person.crop.circle") }
        }
    }
}

private struct PlaceholderView: View {
    let title: String
    let symbol: String

    var body: some View {
        NavigationStack {
            VStack(spacing: 18) {
                Image(systemName: symbol)
                    .font(.system(size: 44, weight: .light))
                    .accessibilityHidden(true)
                Text(title)
                    .font(.largeTitle.weight(.medium))
                Text(L10n.simulationBadge)
                    .font(.caption.monospaced().weight(.semibold))
                    .foregroundStyle(.secondary)
                Text(L10n.foundationMessage)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
            }
            .padding()
            .navigationTitle(L10n.appName)
        }
    }
}
