-- Phase 9: Materialized view for period-based P&L reporting.
-- Groups journal_lines by account, calendar year, and calendar month.
-- Used by GetProfitAndLoss and RefreshViews.
-- Idempotent: CREATE MATERIALIZED VIEW IF NOT EXISTS, CREATE UNIQUE INDEX IF NOT EXISTS.

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_account_period_balances AS
SELECT
    c.id                                          AS company_id,
    a.id                                          AS account_id,
    a.code                                        AS account_code,
    a.name                                        AS account_name,
    a.type                                        AS account_type,
    EXTRACT(YEAR  FROM je.posting_date)::int      AS year,
    EXTRACT(MONTH FROM je.posting_date)::int      AS month,
    SUM(jl.debit_base)                            AS debit_total,
    SUM(jl.credit_base)                           AS credit_total,
    SUM(jl.debit_base) - SUM(jl.credit_base)      AS net_balance
FROM journal_entries je
JOIN journal_lines  jl ON jl.entry_id   = je.id
JOIN accounts       a  ON a.id          = jl.account_id
JOIN companies      c  ON c.id          = a.company_id
GROUP BY
    c.id, a.id, a.code, a.name, a.type,
    EXTRACT(YEAR  FROM je.posting_date),
    EXTRACT(MONTH FROM je.posting_date);

-- Required for REFRESH MATERIALIZED VIEW CONCURRENTLY.
CREATE UNIQUE INDEX IF NOT EXISTS idx_mv_period_balances
    ON mv_account_period_balances (company_id, account_id, year, month);
