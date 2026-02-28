-- Phase 10: Materialized view for all-time (cumulative) account balances.
-- Used by the Balance Sheet report and RefreshViews.
-- Idempotent: CREATE MATERIALIZED VIEW IF NOT EXISTS, CREATE UNIQUE INDEX IF NOT EXISTS.

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_trial_balance AS
SELECT
    c.id                                          AS company_id,
    a.id                                          AS account_id,
    a.code                                        AS account_code,
    a.name                                        AS account_name,
    a.type                                        AS account_type,
    SUM(jl.debit_base)                            AS total_debit,
    SUM(jl.credit_base)                           AS total_credit,
    SUM(jl.debit_base) - SUM(jl.credit_base)      AS net_balance
FROM journal_entries je
JOIN journal_lines  jl ON jl.entry_id   = je.id
JOIN accounts       a  ON a.id          = jl.account_id
JOIN companies      c  ON c.id          = a.company_id
GROUP BY c.id, a.id, a.code, a.name, a.type;

-- Required for REFRESH MATERIALIZED VIEW CONCURRENTLY.
CREATE UNIQUE INDEX IF NOT EXISTS idx_mv_trial_balance
    ON mv_trial_balance (company_id, account_id);
