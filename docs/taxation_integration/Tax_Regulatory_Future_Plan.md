# Tax & Regulatory Implementation — Future Plan

> **Status**: DEFERRED — not part of the production MVP.
> **Purpose**: Documents the full tax compliance roadmap for India (GST, TDS, TCS, GSTR) to be implemented after the MVP is stable.
> **Prerequisite to starting any phase here**: All phases in `One_final_implementation_plan.md` must be complete and stable in production.
> **Last updated**: 2026-02-27

---

## Why This Is Deferred

The tax phases are under-specified. Before any of these phases can begin, the following must be done:

1. **Phase 26 (RCM)**: Document exact RCM categories, vendor types that trigger it, ITC claim timing rules, and expected GSTR-3B output per scenario.
2. **Phase 30 (GSTR export)**: Obtain government GSTR-1 and GSTR-3B JSON schema specs. Define expected output for B2B, B2CS, CDNR, and HSN Summary sections with concrete test data.
3. **Phase 27 (TDS thresholds)**: Specify per-payment vs annual threshold rules for each section code (194C has both ₹30,000 per-payment AND ₹1,00,000 annual). Define FY boundary rollover (cumulative reset on 1 April). Clarify TDS on advance payments.
4. **Multi-currency × GST**: Define how RBI reference rate applies to GST valuation on foreign-currency invoices. May require a separate `gst_exchange_rate` column.

**Before coding begins on any phase in this document**: expand the relevant section, write test scenarios, and get sign-off.

---

## Migration Map

| File | Phase | Description |
|------|-------|-------------|
| 023_tax_rates.sql | Phase 22 | `tax_rates`, `tax_rate_components` tables; HSN/GSTIN columns on products/customers |
| 024_seed_tax_accounts.sql | Phase 22 | CGST Payable, SGST Payable, IGST Payable, ITC accounts |
| 025_sales_order_tax_lines.sql | Phase 23 | `sales_order_tax_lines` table |
| 026_gst_rates.sql | Phase 25 | `indian_state_codes`, GST slabs (0/5/12/18/28) with CGST+SGST/IGST variants |
| 027_tds.sql | Phase 27 | `tds_sections`, `tds_vendor_ledger`, TDS/TCS columns on vendors/customers |
| 028_accounting_periods.sql | Phase 29 | `accounting_periods` table for period locking |

> **Note**: Migration numbers above assume `One_final_implementation_plan.md` completes at migration 022. Confirm the last-applied migration before starting Phase 22.

---

## Phase 22 — Tax Rate Schema + TaxEngine Service

**Goal**: Create the generic tax data model and computation engine. No changes to invoicing yet.

**Pre-requisites**: Phase 7 (RuleEngine for tax account resolution).

### Migrations

**`023_tax_rates.sql`**
```sql
CREATE TABLE IF NOT EXISTS tax_rates (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    code VARCHAR(20) NOT NULL,
    name VARCHAR(100) NOT NULL,
    jurisdiction VARCHAR(20) NULL,
    is_active BOOL DEFAULT true,
    UNIQUE(company_id, code)
);

CREATE TABLE IF NOT EXISTS tax_rate_components (
    id SERIAL PRIMARY KEY,
    tax_rate_id INT NOT NULL REFERENCES tax_rates(id),
    component_name VARCHAR(50) NOT NULL,
    rate NUMERIC(6,4) NOT NULL,
    tax_account_code VARCHAR(20) NOT NULL,
    is_input_tax BOOL DEFAULT false
);

ALTER TABLE products  ADD COLUMN IF NOT EXISTS hsn_code VARCHAR(8) NULL;
ALTER TABLE products  ADD COLUMN IF NOT EXISTS tax_category VARCHAR(20) NULL;
ALTER TABLE products  ADD COLUMN IF NOT EXISTS default_tax_rate_id INT NULL REFERENCES tax_rates(id);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS gstin VARCHAR(15) NULL;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS tax_jurisdiction VARCHAR(10) NULL;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS is_sez BOOL DEFAULT false;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS is_composition_dealer BOOL DEFAULT false;
```

**`024_seed_tax_accounts.sql`** — add to CoA for Company 1000:
```sql
INSERT INTO accounts (company_id, code, name, account_type) VALUES
  (1000_company_id, '2100', 'CGST Payable',  'LIABILITY'),
  (1000_company_id, '2110', 'SGST Payable',  'LIABILITY'),
  (1000_company_id, '2120', 'IGST Payable',  'LIABILITY'),
  (1000_company_id, '1301', 'ITC-CGST',      'ASSET'),
  (1000_company_id, '1311', 'ITC-SGST',      'ASSET'),
  (1000_company_id, '1321', 'ITC-IGST',      'ASSET')
ON CONFLICT DO NOTHING;
```

### Domain

`internal/core/tax_engine.go`:

```go
type TaxComponent struct {
    ComponentName  string
    Rate           decimal.Decimal
    TaxableAmount  decimal.Decimal
    TaxAmount      decimal.Decimal
    TaxAccountCode string
    IsInputTax     bool
}

type TaxEngine interface {
    ComputeOutputTax(ctx context.Context, companyID, taxRateID int, taxableAmount decimal.Decimal) ([]TaxComponent, error)
}
```

Implementation: fetch `tax_rate_components` for the rate; compute `TaxAmount = taxableAmount × rate`; return components. No hardcoded component names.

If `taxRateID = 0` or product has no `default_tax_rate_id`: return empty slice (zero tax — valid for exempt items).

### Tasks

- [ ] `migrations/023_tax_rates.sql`
- [ ] `migrations/024_seed_tax_accounts.sql`
- [ ] `internal/core/tax_engine.go` — `TaxEngine` interface + `taxEngine` implementation
- [ ] Wire `TaxEngine` into `NewAppService`
- [ ] Unit tests: correct components returned, multiple components, zero tax on nil rate
- [ ] All 39 existing tests still pass

### Acceptance Criteria

- `TaxEngine.ComputeOutputTax()` computes correctly for single and multiple components
- No changes to invoicing behaviour yet
- All existing tests pass

---

## Phase 23 — Tax-Aware Invoice Posting

**Goal**: Update `InvoiceOrder()` to post separate journal lines for each tax component.

**Pre-requisites**: Phase 22 complete.

> **Warning**: This phase breaks existing invoice integration tests. Update them as part of this phase.

### Migration

**`025_sales_order_tax_lines.sql`**
```sql
CREATE TABLE IF NOT EXISTS sales_order_tax_lines (
    id SERIAL PRIMARY KEY,
    order_line_id INT NOT NULL REFERENCES sales_order_lines(id),
    tax_rate_component_id INT NOT NULL REFERENCES tax_rate_components(id),
    taxable_amount NUMERIC(14,2) NOT NULL,
    tax_amount NUMERIC(14,2) NOT NULL
);
```

### Changes to `OrderService`

Inject `TaxEngine` into `OrderService`.

Refactor `InvoiceOrder()`:
- Per line: look up `product.default_tax_rate_id`. Call `TaxEngine.ComputeOutputTax()`.
- Compute gross total = net total + all tax amounts.
- Build proposal: `DR AR (gross)` / `CR Revenue per line (net)` / `CR tax_account_code per component`.
- Insert rows into `sales_order_tax_lines`.
- Products with no `default_tax_rate_id`: invoice unchanged (zero tax path).

### Tasks

- [ ] `migrations/025_sales_order_tax_lines.sql`
- [ ] Inject `TaxEngine` into `OrderService` constructor
- [ ] Refactor `InvoiceOrder()` as above
- [ ] Update all integration tests that assert exact AR amounts or proposal line counts
- [ ] New integration test: invoice a product with GST18 → verify tax account credited, AR debited at gross
- [ ] Verify: invoicing a product with no tax rate works exactly as before

### Acceptance Criteria

- Tax-bearing products create split journal entries (revenue + tax components)
- Tax-exempt products unchanged
- All tests pass (updated for new line counts)

---

## Phase 24 — Input Tax Credit on Purchases

**Goal**: Book Input Tax Credit (ITC) when receiving a purchase order.

**Pre-requisites**: Phase 23 (TaxEngine + tax accounts exist). Phase 13 (`ReceivePO` exists).

### Changes

Add to `TaxEngine`:
```go
ComputeInputTax(ctx context.Context, companyID, taxRateID int, taxableAmount decimal.Decimal) ([]TaxComponent, error)
```
Same logic as output tax but `is_input_tax = true` components use ITC accounts (`1301`, `1311`, `1321`).

Add `default_tax_rate_id INT NULL` to `purchase_order_lines` (allow per-line tax rate override).

Update `RecordVendorInvoice()`: per line, call `TaxEngine.ComputeInputTax()`. Post:
- `DR Inventory (net) / DR ITC account per component / CR AP (gross)`

### Tasks

- [ ] `ComputeInputTax` on `TaxEngine` interface + implementation
- [ ] Add `default_tax_rate_id` to `purchase_order_lines` (additive migration — no number reserved, add to `022_po_link.sql` or new file)
- [ ] Update `RecordVendorInvoice()` to post ITC components
- [ ] Integration test: receive PO with GST18 product → ITC accounts debited, AP credited at gross, net inventory cost excludes recoverable tax

### Acceptance Criteria

- ITC correctly booked on vendor invoice
- Net inventory cost excludes recoverable tax
- AP credited at gross amount (including tax)

---

## Phase 25 — GST Rate Seeds + Jurisdiction Resolver

**Goal**: Configure Indian GST slabs and automatically choose CGST+SGST vs IGST based on supply type.

**Pre-requisites**: Phases 23 and 24 in use.

> **Expand before coding**: Define multi-currency × GST valuation. Specify exact RBI rate lookup or stored `gst_exchange_rate` field approach.

### Migration: `026_gst_rates.sql`

```sql
CREATE TABLE IF NOT EXISTS indian_state_codes (
    code CHAR(2) PRIMARY KEY,
    state_name VARCHAR(100) NOT NULL
);
-- Seed all 37 state/UT codes

ALTER TABLE companies ADD COLUMN IF NOT EXISTS state_code CHAR(2) NULL REFERENCES indian_state_codes(code);

-- Seed tax_rates for Company 1000: GST0, GST5, GST12, GST18, GST28
-- Each with intrastate (CGST+SGST) and interstate (IGST) variants
-- Seed tax_rate_components with correct rates and account codes per variant
```

### Domain

`internal/core/gst_resolver.go`:

```go
func ResolveGSTRateID(ctx context.Context, db *pgxpool.Pool, companyID int, customer Customer, gstSlabCode string) (taxRateID int, err error)
```
- Fetch `company.state_code`. If blank, return error.
- Compare with `customer.tax_jurisdiction`. Same state → intrastate rate ID. Different → interstate rate ID.
- SEZ (`customer.is_sez = true`) → GST0 rate ID.

Update `InvoiceOrder()`: when company has `state_code` and customer has `tax_jurisdiction`, call `ResolveGSTRateID()` to override `product.default_tax_rate_id`.

### AI Tools

| Tool | Type | Description |
|------|------|-------------|
| `check_gst_rate` | read | Resolve applicable GST rate for a customer + product pair |
| `check_hsn_coverage` | read | Verify all products have HSN codes set |

### Tasks

- [ ] `migrations/026_gst_rates.sql` — state codes, company state_code column, GST rate seeds
- [ ] `internal/core/gst_resolver.go`
- [ ] Update `InvoiceOrder()` to call resolver
- [ ] Integration tests: same-state → CGST+SGST; different-state → IGST; SEZ → zero rate
- [ ] Register 2 AI tools

### Acceptance Criteria

- CGST+SGST vs IGST auto-resolved from company and customer state codes
- SEZ customers always get zero-rate
- AI agent can check GST applicability via `check_gst_rate` tool

---

## Phase 26 — GST Special Cases (RCM, Composition Dealers, HSN Validation)

**Goal**: Handle Reverse Charge Mechanism, composition dealers, and HSN warnings.

**Pre-requisites**: Phase 25.

> **Expand before coding**: Document exact RCM categories, which vendor types trigger RCM, ITC claim timing, and expected GSTR-3B output for RCM supplies.

### Changes

Add `rcm_applicable BOOL DEFAULT false` to `vendors`.

Update `RecordVendorInvoice()`: if `vendor.rcm_applicable = true`, post self-assessment entry:
`DR RCM Input Tax / CR RCM Output Tax`
(Add `RCM_INPUT_TAX` and `RCM_OUTPUT_TAX` accounts to CoA. Net effect is zero but required for GSTR-3B.)

Update `InvoiceOrder()`: if `customer.is_composition_dealer = true`, skip `TaxEngine` call — no output tax posted.

HSN validation: if `product.hsn_code` is blank, log a warning (do not block the invoice).

### Tasks

- [ ] Add `rcm_applicable` to `vendors` table (additive migration or add to `019_vendors.sql` if not yet applied)
- [ ] Add RCM accounts to CoA seed migration
- [ ] Update `RecordVendorInvoice()` for RCM
- [ ] Update `InvoiceOrder()` for composition dealer bypass
- [ ] HSN warning log in `InvoiceOrder()`
- [ ] Integration tests: RCM → self-assessment lines present; composition customer → no tax lines

### Acceptance Criteria

- RCM vendor invoice posts self-assessment lines (`DR RCM Input Tax / CR RCM Output Tax`)
- Composition customer invoice has no tax lines
- HSN missing → warning logged, invoice not blocked

---

## Phase 27 — TDS Schema + Deduction on Vendor Payments

**Goal**: Deduct TDS at source when paying vendors above the threshold.

**Pre-requisites**: Phase 14 (`PayVendor` exists). Phase 7 (TDS Payable via RuleEngine).

> **Expand before coding**: Specify per-payment vs annual threshold per section (194C: both ₹30,000 per-payment AND ₹1,00,000 annual). Define FY boundary rollover logic (cumulative reset on 1 April). Clarify TDS on advance payments vs invoice time.

### Migration: `027_tds.sql`

```sql
CREATE TABLE IF NOT EXISTS tds_sections (
    id SERIAL PRIMARY KEY,
    code VARCHAR(10) NOT NULL UNIQUE,
    description TEXT NOT NULL,
    rate NUMERIC(6,4) NOT NULL,
    threshold_limit NUMERIC(14,2) NOT NULL
);

ALTER TABLE vendors ADD COLUMN IF NOT EXISTS tds_applicable BOOL DEFAULT false;
ALTER TABLE vendors ADD COLUMN IF NOT EXISTS default_tds_section_id INT NULL REFERENCES tds_sections(id);

CREATE TABLE IF NOT EXISTS tds_vendor_ledger (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    vendor_id INT NOT NULL REFERENCES vendors(id),
    section_id INT NOT NULL REFERENCES tds_sections(id),
    financial_year INT NOT NULL,
    cumulative_paid NUMERIC(14,2) DEFAULT 0,
    UNIQUE(company_id, vendor_id, section_id, financial_year)
);
```

Seed TDS sections:
- `194C` — Contractors, 1%, threshold ₹30,000 per-payment / ₹1,00,000 annual
- `194J` — Professional Services, 10%, threshold ₹30,000

Add account rules seed: `TDS_PAYABLE` → account `2200`; `TCS_PAYABLE` → account `2210`.

### Logic in `PayVendor()`

If `vendor.tds_applicable` and `default_tds_section_id` set:
1. Lock + read `tds_vendor_ledger` row for (vendor, section, current FY). Create if absent.
2. If `cumulative_paid < threshold_limit`: no TDS deduction; update cumulative only.
3. If threshold crossed: `tds_amount = payment_amount × section.rate`.
4. Post: `DR AP (full) / CR Bank (net) / CR TDS Payable (deducted amount)`.
5. Update `tds_vendor_ledger.cumulative_paid += payment_amount`.

### AI Tool

| Tool | Type | Description |
|------|------|-------------|
| `get_tds_threshold_status` | read | Cumulative paid vs threshold for a vendor + section + FY |

### Tasks

- [ ] `migrations/027_tds.sql`
- [ ] Seed 194C and 194J sections
- [ ] Add TDS_PAYABLE + TCS_PAYABLE account rules to seed migration
- [ ] Update `PayVendor()` with TDS logic
- [ ] Register `get_tds_threshold_status` AI tool
- [ ] Integration tests: first payment below threshold → no TDS; second payment crosses threshold → TDS deducted; verify split entry amounts

### Acceptance Criteria

- TDS deducted only after threshold crossed
- Correct split: AP debited in full, Bank credited net, TDS Payable credited for deducted amount
- `tds_vendor_ledger.cumulative_paid` accumulates correctly across multiple payments

---

## Phase 28 — TCS on Customer Receipts + TDS/TCS Settlement

**Goal**: Mirror TDS for customer collections (TCS). Enable settlement payments to the government.

**Pre-requisites**: Phase 27.

### Changes

Add to `customers`:
```sql
ALTER TABLE customers ADD COLUMN IF NOT EXISTS tcs_applicable BOOL DEFAULT false;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS default_tcs_section_id INT NULL REFERENCES tds_sections(id);
```

Create `tcs_customer_ledger` (same schema as `tds_vendor_ledger`, keyed by customer).

Update `RecordPayment()` in `OrderService`: if `customer.tcs_applicable`:
- Compute TCS; post `DR Bank (gross) / CR AR (net) / CR TCS Payable (collected)`.
- Update `tcs_customer_ledger.cumulative_collected`.

New `ComplianceService`:
- `SettleTDS(ctx, companyCode, sectionCode, period, bankCode string, ledger Ledger) error` — posts `DR TDS Payable / CR Bank` for the net TDS balance for that section/period.
- `SettleTCS(ctx, companyCode, sectionCode, period, bankCode string, ledger Ledger) error` — mirror.

### ApplicationService additions

```go
SettleTDS(ctx context.Context, req TDSSettlementRequest) (*SettlementResult, error)
SettleTCS(ctx context.Context, req TCSSettlementRequest) (*SettlementResult, error)
```

### AI Tools

| Tool | Type |
|------|------|
| `get_tcs_status` | read |
| `settle_tds` | write |
| `settle_tcs` | write |

### Tasks

- [ ] Add TCS columns to `customers`
- [ ] Create `tcs_customer_ledger` table
- [ ] Update `RecordPayment()` for TCS
- [ ] Implement `ComplianceService` with `SettleTDS` + `SettleTCS`
- [ ] `ApplicationService` additions
- [ ] Register 3 AI tools
- [ ] Integration tests: TCS collected on receipt; settlement zeroes TCS Payable balance

### Acceptance Criteria

- TCS collected on customer payments above threshold
- `SettleTDS` and `SettleTCS` clear the tax payable balance
- AI agent can propose TDS/TCS settlement via tool calls

---

## Phase 29 — Period Locking

**Goal**: Prevent journal entries from being posted to a closed accounting period.

**Pre-requisites**: Phase 9 (reporting understands periods). Can be implemented after MVP without ordering dependency.

### Migration: `028_accounting_periods.sql`

```sql
CREATE TABLE IF NOT EXISTS accounting_periods (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    year INT NOT NULL,
    month INT NOT NULL,
    status VARCHAR(10) NOT NULL DEFAULT 'OPEN',
    locked_at TIMESTAMPTZ,
    locked_by VARCHAR(100),
    UNIQUE(company_id, year, month)
);
```

### Logic

Update `Ledger.executeCore()`: before inserting the journal entry, check for a `LOCKED` row for the `posting_date`'s year/month. If found, return: `"posting to locked period YYYY-MM is not allowed"`.

Add to `ReportingService`:
- `LockPeriod(ctx, companyCode string, year, month int) error`
- `UnlockPeriod(ctx, companyCode string, year, month int) error`

### ApplicationService additions

```go
LockPeriod(ctx context.Context, companyCode string, year, month int) error
UnlockPeriod(ctx context.Context, companyCode string, year, month int) error
```

### AI Tools

| Tool | Type |
|------|------|
| `check_period_lock` | read |
| `lock_period` | write |
| `unlock_period` | write |

### Tasks

- [ ] `migrations/028_accounting_periods.sql`
- [ ] Update `Ledger.executeCore()` to check period lock
- [ ] `LockPeriod` + `UnlockPeriod` on `ReportingService` + `ApplicationService`
- [ ] Register 3 AI tools
- [ ] Integration tests: post → lock → attempt re-post to same period → expect error; unlock → post succeeds

### Acceptance Criteria

- Posting to a locked period fails with clear error message
- Unlocking re-enables posting
- All periods default to OPEN (no retroactive locking of existing data)

---

## Phase 30 — GSTR-1 + GSTR-3B Export

**Goal**: Export GST return data as JSON/CSV in government-prescribed format.

**Pre-requisites**: Phase 25 (GST tax lines in `sales_order_tax_lines`). Phase 29 (period locking — return data should come from a locked period).

> **Expand before coding**: Obtain official GSTR-1 and GSTR-3B JSON schema from GSTN. Define expected output for B2B, B2CS, CDNR, and HSN Summary sections using a concrete test dataset. Note: CDNR requires credit notes — this feature is not yet planned.

### Data Structures

```go
type GSTR1Report struct {
    CompanyGSTIN string
    Period       string  // "022026" format
    B2B          []B2BInvoice
    B2CS         []B2CSAggregate
    CDNR         []CreditDebitNote  // requires credit note feature
    HSNSummary   []HSNLine
}

type GSTR3BReport struct {
    CompanyGSTIN    string
    Period          string
    OutputTaxCGST   decimal.Decimal
    OutputTaxSGST   decimal.Decimal
    OutputTaxIGST   decimal.Decimal
    ITCClaimed      map[string]decimal.Decimal
    NetTaxPayable   map[string]decimal.Decimal
}
```

### Domain additions to `ReportingService`

- `ExportGSTR1(ctx, companyCode string, year, month int) (*GSTR1Report, error)` — query `sales_orders` + `sales_order_tax_lines` + `customers` for period; structure into B2B, B2CS, HSN Summary.
- `ExportGSTR3B(ctx, companyCode string, year, month int) (*GSTR3BReport, error)` — aggregate output tax liability per component + ITC from journal_lines.

### AI Tools

| Tool | Type |
|------|------|
| `get_gstr1_preview` | read |
| `get_gstr3b_preview` | read |
| `export_gstr1` | write (generates file) |
| `export_gstr3b` | write (generates file) |

### Tasks

- [ ] Define `GSTR1Report`, `GSTR3BReport` structs matching government JSON schema
- [ ] Implement `ExportGSTR1` on `ReportingService`
- [ ] Implement `ExportGSTR3B` on `ReportingService`
- [ ] `ApplicationService` additions + web download endpoints
- [ ] Register 4 AI tools
- [ ] Integration test: known dataset → GSTR1 B2B total matches invoice total; HSN summary totals match line totals

### Acceptance Criteria

- GSTR-1 B2B section groups invoices by customer GSTIN with correct taxable value and tax breakdown
- GSTR-3B output tax matches sum of `sales_order_tax_lines` for the period
- HSN summary totals reconcile to invoice line totals
- Export file downloadable as JSON

---

## Implementation Order

```
Phase 22 (Tax schema + TaxEngine)
    ↓
Phase 23 (Tax-aware invoicing)
    ↓
Phase 24 (Input tax / ITC on purchases)
    ↓
Phase 25 (GST slabs + jurisdiction resolver)
    ↓
Phase 26 (RCM + composition dealers + HSN warnings)
    ↓
Phase 27 (TDS deduction on vendor payments)
    ↓
Phase 28 (TCS on receipts + TDS/TCS settlement)
    ↓
Phase 29 (Period locking)   ← can run in parallel with 27–28
    ↓
Phase 30 (GSTR-1 + GSTR-3B export)
```

---

## Known Gaps (Must Resolve Before Coding)

| Gap | Phase | Required Action |
|-----|-------|-----------------|
| RCM categories and timing | 26 | Document exact vendor types, ITC claim timing, GSTR-3B impact |
| GSTR-1 format specification | 30 | Obtain GSTN JSON schema; define B2B/B2CS/CDNR/HSN test cases |
| GSTR-3B format specification | 30 | Define exact aggregation logic from tax lines |
| TDS per-payment vs annual threshold | 27 | Specify 194C dual-threshold logic; define FY rollover |
| TDS on advance payments | 27 | Define when TDS applies — at PO time, GR time, or payment time |
| Multi-currency GST valuation | 25 | Define RBI rate lookup or `gst_exchange_rate` column approach |
| Credit notes (CDNR) | 30 | Credit note feature not yet planned; CDNR in GSTR-1 is blocked |
| HSN codes on existing products | 25 | All products in `products` table must have `hsn_code` populated before GSTR export |
