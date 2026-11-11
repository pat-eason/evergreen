# Evergreen — financial calculator & budgeting app

A local-first financial planner built around three ideas — **Contribution Crossover**, **Coast FIRE** and **Financial Freedom** — plus a monthly household budget, a Monte Carlo retirement planner with a cash-flow table, and a tax-aware **Smart Withdrawal** strategy.

| Layer    | Tech                                                                 |
| -------- | -------------------------------------------------------------------- |
| Frontend | Vue 3 + TypeScript (Vite), hand-rolled SVG charts, no UI framework   |
| API      | Go (`net/http`), cookie sessions, bcrypt                             |
| Database | SQLite (pure-Go `modernc.org/sqlite`, no CGO), stored in a volume    |
| Packaging| Docker Compose (nginx serves the SPA and proxies `/api`)             |

## Run it (Docker)

```bash
docker compose up --build
```

Open <http://localhost:8080>, create an account, and start with **Accounts → Add account**.

Data lives in the `wellness-data` Docker volume (`/data/wellness.db` in the API container), so it survives restarts and rebuilds. To back it up:

```bash
docker compose cp api:/data/wellness.db ./wellness-backup.db
```

To wipe everything: `docker compose down -v`.

If you serve the app over HTTPS, set `COOKIE_SECURE: "true"` on the `api` service.

## Run it (development)

Requires Go 1.26+, Node 22+ and pnpm.

```bash
# terminal 1 — API on :8081, SQLite at backend/data/wellness.db
cd backend && go run .

# terminal 2 — Vite dev server on :5173 (proxies /api to :8081)
cd frontend && pnpm install && pnpm dev
```

Tests and type checks:

```bash
cd backend && go test ./...
cd frontend && pnpm typecheck && pnpm build
```

## What's in the app

- **Accounts** — record cash, investments, mortgage, credit cards and debts. Each category has its own settings:
  - *Cash*: balance, APY, monthly deposit
  - *Investments*: tax treatment (taxable / tax-deferred / tax-free), contributions, employer match, expected return, fees, cost basis
  - *Mortgage*: balance, rate, term, payment, home value & appreciation, property tax, insurance
  - *Credit card*: balance, APR, limit, payment, paid-in-full toggle
  - *Debt*: type, balance, rate, term, payment
- **Budget** — one budget per month with Income / Savings / Expenses, spreadsheet-style formula amounts (`=(1368.23*2)+500`), growth estimates per savings line, annual outlook, month-to-month copy, a starter template modelled on `Budget.xlsx`, and "Use in retirement plan" to push income/expenses and linked savings contributions into your accounts and profile.
- **Plan** — Monte Carlo projection (median vs 10th percentile), probability of success, sustainable spending, income events (Social Security, pension, part-time…), spending goals (health care, travel…), and a detailed cash-flow table. Snapshots can be saved.
- **Smart Withdrawal** — year-by-year retirement income sources (income events, taxable, tax-deferred, tax-free), estimated taxes, budgeted spending and portfolio balance, with expandable detail, RMDs, optional Roth conversions, and a side-by-side strategy comparison.
- **Freedom** — Contribution Crossover, Coast FIRE and Financial Freedom with what-if sliders (retirement age, contributions, spending, return, SWR, inflation) and saved snapshots.
- **Auth** — username/password registration and sign-in; sessions are HttpOnly cookies, passwords are bcrypt-hashed, and every record is scoped to its owner.

## How the simulations work

All of it lives in [`backend/internal/sim`](backend/internal/sim).

- **Returns.** Each year's market return is log-normal with the mean and volatility you set (shared market shock across investment buckets; cash earns its APY). The "median" line is the median-market path (z = 0); the 10th percentile comes from 1,000 seeded trials, so results are reproducible.
- **Buckets.** Balances are tracked as cash, taxable (with cost basis), tax-deferred and tax-free, each with its own weighted expected return.
- **Liabilities.** Mortgages, cards and debts are amortised monthly. Before retirement they are paid from your paycheck; after retirement the payments (plus property tax & insurance) become spending that the portfolio must fund.
- **Taxes.** 2025 US federal brackets (single/married) indexed to inflation, standard deduction, long-term gains/qualified dividends stacking (0/15/20%), Social Security taxability (IRS provisional-income rules), a flat state rate, RMDs (age 73/75), and an optional Roth-conversion bracket target.
- **Withdrawals.** Each year the solver iterates until after-tax cash meets spending. *Smart* fills the 12% bracket with tax-deferred money first, then taxable, then the rest, Roth last; *Taxable first* and *Tax-deferred first* are available for comparison.
- **Success & ability.** Success rate = share of trials that fully fund spending through the plan age. Sustainable spending is found by bisection on a spending multiplier at a 90% success target.
- **FIRE math** (today's dollars):
  - *FI number* = annual retirement spending ÷ safe withdrawal rate (includes ongoing goals like health care and property tax).
  - *Contribution Crossover* = first age at which a year's investment growth exceeds a year's contributions.
  - *Coast FIRE number* = FI number ÷ (1 + real return)^(years to retirement); you're coasting when invested assets ≥ that.

> These are planning estimates, not tax or investment advice. Tax law is simplified (e.g. no itemised deductions, IRMAA, NIIT or state-specific rules).

## API

All routes are under `/api`, JSON, cookie-authenticated except `auth/*` and `health`.

`POST auth/register|login|logout` · `GET me` · `GET|PUT profile` · `GET|POST accounts`, `PUT|DELETE accounts/{id}` · `GET|POST events`, `PUT|DELETE events/{id}` · `GET budgets`, `GET|PUT|DELETE budgets/{YYYY-MM}`, `POST budgets/{month}/copy|apply` · `POST plan/simulate` · `POST freedom/analyze` · `GET|POST calculations`, `DELETE calculations/{id}`
