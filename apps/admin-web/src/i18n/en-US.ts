export const enUS = {
  appName: "Cytisus Admin",
  eyebrow: "Internal operations · Simulated banking",
  title: "Review with a second set of eyes.",
  description:
    "Investigate same-name bank withdrawal cases, record evidenced proposals, require an independent checker, and reconcile provider totals to the Ledger.",
  environmentLabel: "SIMULATED LOCAL",
  environmentNotice:
    "Headers below are synthetic local identities. Production access requires isolated enterprise SSO and is not provided by this console.",
  queueTitle: "Compliance case queue",
  queueDescription:
    "Cases expose policy reasons and next actions without internal risk thresholds.",
  queueStatusLabel: "Case status",
  queueAllOption: "All active cases",
  queueLoadAction: "Load cases",
  queueLoading: "Loading authorized case records…",
  queueEmpty: "No cases match this queue filter.",
  queueNotLoaded: "Choose an operator identity, then load the case queue.",
  selectedCaseLabel: "Selected case",
  customerLabel: "Customer reference",
  caseTypeLabel: "Case type",
  reasonLabel: "Policy reason",
  nextActionLabel: "Next action",
  policyLabel: "Policy version",
  updatedLabel: "Updated",
  makerTitle: "Maker proposal",
  makerDescription:
    "A maker records the proposed action, ticket, and evidence. This step never approves its own proposal.",
  actorIDLabel: "Synthetic operator ID",
  actorRoleLabel: "Operator role",
  proposalActionLabel: "Proposed action",
  proposalApprove: "Approve after cooling",
  proposalOverride: "Override cooling",
  proposalReject: "Reject withdrawal",
  proposalReasonLabel: "Reason code",
  ticketLabel: "Ticket reference",
  evidenceLabel: "Evidence reference",
  proposeAction: "Create review proposal",
  proposing: "Recording proposal…",
  proposalSuccess: "Proposal recorded and awaiting an independent checker.",
  proposalRequired: "Select a case and complete every evidence field.",
  checkerTitle: "Independent checker",
  checkerDescription:
    "The checker must be a different authorized person. Every decision is audited.",
  proposalIDLabel: "Proposal ID",
  decisionLabel: "Decision",
  approveDecision: "Approve proposal",
  rejectDecision: "Reject proposal",
  decisionReasonLabel: "Decision reason",
  decideAction: "Record checker decision",
  deciding: "Applying independent decision…",
  decisionSuccess: "Independent checker decision recorded.",
  differentCheckerRequired:
    "Use a checker identity different from the proposal maker.",
  decisionRequired: "Enter a proposal ID and decision reason.",
  reconciliationTitle: "Bank reconciliation",
  reconciliationDescription:
    "Compare provider-confirmed net cash with banking-related settled Ledger activity. Differences remain visible and open a case.",
  reconciliationCustomerLabel: "Customer reference",
  reconciliationProviderLabel: "Provider amount override (optional)",
  reconciliationProviderHint:
    "Leave blank to use simulator totals. An override is synthetic and intended for difference testing.",
  reconcileAction: "Run reconciliation",
  reconciling: "Reconciling provider and Ledger totals…",
  reconciliationSuccess: "Reconciliation completed and retained for audit.",
  reconciliationRequired: "Enter a customer reference.",
  ledgerAmountLabel: "Ledger",
  providerAmountLabel: "Provider",
  differenceLabel: "Difference",
  resultStatusLabel: "Result",
  dismissError: "Dismiss error",
  errorFallback: "The admin request could not be completed.",
  errorNextAction: "Refresh the queue and retry with an authorized role.",
  footer:
    "Restricted admin surface · Synthetic data only · Every decision audited",
} as const;

export const caseStatusCopy: Record<string, string> = {
  OPEN: "Open",
  INFORMATION_REQUIRED: "Information required",
  IN_REVIEW: "In review",
  APPROVED: "Approved",
  REJECTED: "Rejected",
  CLOSED: "Closed",
};

export const roleCopy: Record<string, string> = {
  COMPLIANCE_ANALYST: "Compliance analyst",
  RISK_ANALYST: "Risk analyst",
  OPERATIONS: "Operations",
  ADMIN: "Administrator",
  AUDITOR: "Auditor (read only)",
  FINANCE: "Finance (reconciliation)",
};

export const reconciliationStatusCopy: Record<string, string> = {
  COMPLETED_WITHOUT_DIFFERENCE: "Completed without difference",
  COMPLETED_WITH_DIFFERENCES: "Completed with differences",
};
