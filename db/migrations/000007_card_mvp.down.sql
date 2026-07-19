DROP SCHEMA notification CASCADE;
DROP SCHEMA card CASCADE;

DROP TRIGGER compliance_case_events_immutable ON compliance.case_events;
DELETE FROM compliance.case_events
WHERE case_id IN (
    SELECT id
    FROM compliance.cases
    WHERE case_type IN ('CARD_DISPUTE', 'CARD_RECONCILIATION')
);
CREATE TRIGGER compliance_case_events_immutable
BEFORE UPDATE OR DELETE ON compliance.case_events
FOR EACH ROW EXECUTE FUNCTION compliance.reject_immutable_mutation();

DELETE FROM compliance.cases
WHERE case_type IN ('CARD_DISPUTE', 'CARD_RECONCILIATION');

ALTER TABLE compliance.cases DROP CONSTRAINT compliance_cases_type_check;
ALTER TABLE compliance.cases ADD CONSTRAINT compliance_cases_type_check
CHECK (case_type IN ('EDD', 'BANK_WITHDRAWAL_REVIEW', 'TRANSACTION_MONITORING_ALERT'));
