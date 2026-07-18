export const enUS = {
  appName: "Cytisus",
  eyebrow: "Simulation-first foundation",
  title: "A clear operating surface for every financial state.",
  description:
    "Phase 0 establishes secure contracts, isolated applications, and local simulators. No real financial service is active.",
  statusLabel: "Environment status",
  statusValue: "SIMULATED",
  apiLabel: "API contract",
  apiValue: "Operational health only",
  providerLabel: "Provider mode",
  providerValue: "Local adapters",
  safetyLabel: "Production safety",
  safetyValue: "Simulators blocked",
  footer: "Cytisus Phase 0 · Synthetic data only",
} as const;

export type CopyKey = keyof typeof enUS;

export function copy(key: CopyKey): string {
  return enUS[key];
}
