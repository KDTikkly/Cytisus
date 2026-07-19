# iOS test evidence

## Local limitation

This Prompt 8 workstation is Windows and has no Swift toolchain or Xcode. No local iOS execution or simulator screenshot is claimed.

## Source and test coverage

The Swift package targets iOS 26 and macOS 26, and the CI job runs on `macos-26`. Its current source includes:

- Paper API models, store, views, idempotent order/replay/cancel commands, quote status labels, portfolio state, and accessibility/localization tests;
- Card API models, store, lifecycle/terminal views, and tests;
- Crypto OpenAPI-aligned models and decoding tests;
- localization-key tests.

Known gaps are material: Banking, RWA, Passkey/recovery, KYC, Read-only Security Mode, report export, and protective-sell iOS flows are absent; the Crypto UI flow is incomplete.

## Authoritative execution

The GitHub Actions `ios` job runs on macOS and executes:

```text
swift test
xcodebuild -list
xcodebuild -scheme Cytisus -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build
```

The final Prompt 8 Draft PR CI URL and conclusion will be recorded after the branch is pushed. A passing package build is evidence of compilation/tests, not App Store, device, signing, Wallet entitlement, or production readiness.
