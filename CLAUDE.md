# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Evergreen: a local-first budgeting + retirement-planning app. Vue 3/TypeScript SPA (`frontend/`), Go JSON API with PostgreSQL (`backend/`), packaged with Docker Compose. See `README.md` for the feature list and the simulation methodology.

## Commands

```bash
# Full stack (http://localhost:8080; Caddy serves the SPA, proxies /api -> api:8081 and does HTTPS)
docker compose up -d --build            # rebuild after code changes (use `--build web` / `api` for one side)
docker compose down -v                  # DELETES the Postgres volume (all users/data) — omit -v to keep data

# Backend (Go 1.26, cwd backend/)
docker compose up -d db                 # Postgres on 127.0.0.1:5432 (user/db/password: evergreen)
DATABASE_URL='postgres://evergreen:evergreen@localhost:5432/evergreen?sslmode=disable' go run .   # API on :8081 (env: ADDR, DATABASE_URL, COOKIE_SECURE, ALLOW_REGISTRATION)
# database tests skip unless TEST_DATABASE_URL is set; start a throwaway Postgres for them:
docker run -d --rm --name pgtest -e POSTGRES_PASSWORD=test -p 127.0.0.1:54329:5432 postgres:17-alpine
export TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:54329/postgres?sslmode=disable'
go vet ./... && go test ./...
go test ./internal/sim -run TestFreedom -v      # single test
go test ./internal/api -run TestHouseholdAndOwnership -v

# Frontend (cwd frontend/, pnpm)
pnpm dev                                # Vite on :5173, proxies /api to localhost:8081 (override with API_URL)
pnpm build                              # vue-tsc --noEmit && vite build  <- run this, not just `pnpm typecheck`
```

There is no linter or frontend test suite. Deployment to a droplet is documented in `DEPLOY.md`; `.github/workflows/deploy.yml` tests on every PR and, on `main`, builds/pushes the images to GHCR and runs `deploy/deploy.sh` on the droplet over SSH (which backs up the database first). `deploy/backup.sh` / `deploy/restore.sh` wrap `pg_dump` / `pg_restore`; to test compose changes without touching real data use a throwaway project (`COMPOSE_PROJECT_NAME=fwtest HTTP_PORT=18080 HTTPS_PORT=18443 POSTGRES_PORT=15432 docker compose up -d --build`, then `down -v` of *that* project only). `.claude/launch.json` defines `api` (`go -C backend run .`) and `web` preview servers.

## Gotchas

- **`pnpm typecheck` passes things `pnpm build` rejects.** vue-tsc does not validate template expressions: an `if` statement in an `@event` handler, or a template literal with nested quotes in an attribute, only fails in the Vite build (which is what the Docker image runs). Move non-trivial expressions into `<script setup>` computeds/functions.
- TypeScript is pinned to `~5.9` (vue-tsc 3.x breaks on TS 7) and `frontend/package.json` pins `packageManager: pnpm@10.33.0` (Corepack in Docker otherwise grabs pnpm 12, which rejects the lockfile). The root `package.json` is a leftover from `pnpm init` and unused.
- Rates are decimals everywhere (0.07 = 7%); the UI's `NumField kind="pct"` converts for display.

## Architecture

### Backend (`backend/`)
- `main.go` wires `store` -> `api.Server`. `internal/store` is the only SQL; `internal/api` is handlers + auth; `internal/sim` is pure planning math with no I/O.
- **Migrations are the `migrations` slice in `store/store.go`**: applied once each, in order, under a Postgres advisory lock, and recorded in `schema_migrations`. **Append** a new entry for any schema change; never edit one that has shipped. An older binary refuses to start against a newer schema (so a rollback after a migration needs the pre-deploy backup restored). Columns are real types (`TIMESTAMPTZ`, `BOOLEAN`, `JSONB` for account config / profile / saved calculations); email uniqueness is a `lower(email)` index. Store methods use `$n` placeholders and `RETURNING id`, and detect unique violations via the pgx error code (`isUnique`).
- **Database tests** use `storetest.New(t)`: a fresh schema per test inside the Postgres at `TEST_DATABASE_URL`, dropped afterwards (tests skip when it is unset; CI provides a Postgres service). The API tests run through it too.
- **Every query is scoped by `user_id`** (cross-user access returns 404). Owner references (account/event `owner`) are validated against the caller's household in `resolveOwner`.
- Deployment behaviour: the API is only reachable through Caddy, which sets `X-Forwarded-For`/`-Proto`; `clientIP` and `secure()` rely on that (cookies get `Secure` automatically over HTTPS). `ALLOW_REGISTRATION=false` closes sign-ups once any user exists (`registrationOpen`; the first account is always allowed). The Caddyfile's CSP only allows same-origin assets, so Vite is configured with `assetsInlineLimit: 0` (inlined `data:` fonts are blocked).
- Auth: opaque random session token in an HttpOnly cookie (only its SHA-256 is stored); bcrypt passwords; in-memory failed-login throttle (also used for password re-verification). `ServeHTTP` rejects non-JSON bodies as a CSRF guard. Email is lower-cased via `normalizeEmail`.
- Account `config` is a single union struct (`sim.AccountConfig`) stored as JSON; `sanitizeAccount` validates and strips fields that don't belong to the account's category.
- Budget amounts may be formulas (`=(1368.23*2)+500`). The evaluator exists twice and must stay in sync: `internal/api/expr.go` (authoritative, runs on save) and `frontend/src/lib/expr.ts` (live display).
- **Age is stored as `birth_year`** (profile and household members); `current_age`/`age` are derived on read (`loadProfile`, `toDTO`) so ages advance on their own. Saving converts age back to a birth year.
- **Event ages belong to their owner.** `loadInputs` shifts a member's event `StartAge` by (primary age − member age) so the engine only ever runs on the primary person's timeline. The UI converts back with `ageShift()` when placing chart markers.

### Ledger (`internal/ledger`, `api/ledger.go`)
- Append-only `ledger` table; account create / balance change / settings edit / delete each add a row (hooked in the account handlers, plus `POST /accounts/{id}/value` for dated "record a value"). `account_id` is deliberately **not** a foreign key so history outlives the account.
- Each row stores the balance **after** the event; `Previous`/`Delta` are derived on read (`Annotate`), so backdated entries never leave stale deltas. `occurred_at` is a `TIMESTAMPTZ`.
- `Build` replays entries into `Point`s (net worth, assets, liabilities, per-category, per-account series). Mortgages carry the home value in `asset_value`. An account's **first** entry is its opening balance and counts as a contribution (even if it was backfilled before the "created" row); later `deposit`/`withdrawal` reasons are flows; everything else is growth/loss. Deleting an account is a withdrawal, so growth already earned is not erased.
- A dated value earlier than the account's latest entry only adds history (`applied: false`); it does not change the live balance.

### Simulation engine (`internal/sim`)
- `Prepare(Inputs)` builds a `Plan` (four buckets: cash / taxable with cost basis / tax-deferred / tax-free; liability schedules; weighted per-bucket returns). `Plan.Simulate(z, scale, detail, starts)` runs one path; `RunPlan` combines the detailed median path with Monte Carlo bands.
- Returns are log-normal. Passing `z == nil` gives the **median-market path** (used for table detail, withdrawal breakdowns and the "median" chart line); MC trials pass seeded normal shocks (deterministic by seed). "Success" = every year's spending fully funded.
- Pre-retirement, expenses/mortgage/debt payments are assumed paid from the paycheck (only savings and user events touch the portfolio); after retirement the portfolio funds retirement spending, user goals, liability payments and property tax/insurance. Cash flow = saving + income − spending − taxes.
- Each year solves withdrawals by fixed-point iteration so after-tax cash meets spending (`tax.go` is a simplified 2025 federal + flat state model, with SS taxability, LTCG stacking, RMDs). Strategies: `smart` (fill 12% bracket from tax-deferred, then taxable, then rest, Roth last), `taxable_first`, `tax_deferred_first`; optional Roth-conversion bracket target.
- `freedom.go` (`AnalyzeFreedom`) is separate, deterministic, in today's dollars: FI number, Coast FIRE number, Contribution Crossover. It assumes you keep contributing through age 80 so each milestone is "earliest age if you keep saving", independent of retirement age. `freedomDefaults` in `api/handlers.go` builds its inputs (FI spending includes ongoing goals + property tax/insurance).
- `POST /api/plan/simulate` accepts profile overrides for what-if runs without persisting; `fast: true` skips the sustainable-spending bisection (used by the strategy comparison).

### Frontend (`frontend/src`)
- No state library: `lib/store.ts` holds one `reactive` `state` (user, accounts, events, members, profile, cached `plan`) plus derived `position`, `people`, `displayName`. Pages call `loadData()` / `ensurePlan()`; mutations call `invalidatePlan()`. After a profile/household change bump `state.profileRev`, which views watch to refetch their own derived data (Dashboard, Plan, Withdrawal, Freedom).
- History page (`views/History.vue`), the Accounts sparklines and the Dashboard card all read `GET /api/ledger/history`; `ValueDrawer` is the "record a value" form. `LineChart` takes `fromZero=false` to zoom timelines to the data, `selectable` (drag across the plot to emit a `select` x-range; History turns it into a custom `from`/`to`) and `tooltipExtra` (extra hover rows such as totals). The history and ledger endpoints take optional inclusive UTC `from`/`to` dates, and `ledger.Build` ignores entries after `To`. `lib/api.ts` is the only fetch wrapper (same-origin cookies; a 401 outside `/auth/*` clears the session). Types in `lib/types.ts` mirror the Go JSON.
- Charts (`LineChart`, `StackedBars`, `DonutChart`, `ProgressRing`) are hand-written SVG with no chart library; styling is a single token-based `styles/main.css` (brand palette in `:root`: primary `#250104`, secondary `#071a06`, accent `#cdf73d`).
- Layout: `AppShell` is a fixed left sidebar at >=1024px (collapsible to an icon rail, preference in localStorage via `ui` in the store) and a top bar below; `App.vue` offsets content with a `.has-nav` / `.collapsed` padding. The profile/account drawers are mounted in `AppShell`, so they are available on every page.
