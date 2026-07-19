UPDATE marketdata.instrument_capabilities
SET rwa_mint_enabled = FALSE,
    policy_version = 'paper-instrument-v1'
WHERE instrument_id = '8f3c34b4-264e-4ccf-92fb-074b0a4b6981';

DROP TRIGGER compliance_case_events_immutable ON compliance.case_events;
DELETE FROM compliance.case_events
WHERE case_id IN (
    SELECT id FROM compliance.cases
    WHERE case_type IN ('RWA_ADDRESS_REVIEW', 'RWA_RECONCILIATION')
);
CREATE TRIGGER compliance_case_events_immutable
BEFORE UPDATE OR DELETE ON compliance.case_events
FOR EACH ROW EXECUTE FUNCTION compliance.reject_immutable_mutation();

DELETE FROM compliance.cases
WHERE case_type IN ('RWA_ADDRESS_REVIEW', 'RWA_RECONCILIATION');

ALTER TABLE compliance.cases DROP CONSTRAINT compliance_cases_type_check;
ALTER TABLE compliance.cases ADD CONSTRAINT compliance_cases_type_check
CHECK (case_type IN (
    'EDD', 'BANK_WITHDRAWAL_REVIEW', 'TRANSACTION_MONITORING_ALERT',
    'CARD_DISPUTE', 'CARD_RECONCILIATION'
));

DROP SCHEMA rwa CASCADE;
DROP TABLE securities.position_reservations;
