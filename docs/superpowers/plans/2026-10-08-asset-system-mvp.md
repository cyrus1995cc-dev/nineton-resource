# Asset System MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first working vertical slice of the self-hosted asset system with Excel import, QR-string lookup, web dashboard, asset list/detail, and auditable asset events for Web, Android-ready responsive UI, and future DingTalk H5 reuse.

**Architecture:** A single Node.js service exposes REST APIs and serves the built Vue 3 frontend. SQLite is used for the local MVP database through Node's built-in `node:sqlite`; the schema and repository boundaries are compatible with a later PostgreSQL migration. The responsive Vue frontend is shared by the web app, future Capacitor Android packaging, and DingTalk H5.

**Tech Stack:** Node.js 24, Express, Vue 3, Vite, TypeScript, SQLite (`node:sqlite`), `xlsx`, Vitest, Playwright smoke checks.

**Spec:** `docs/superpowers/specs/2026-10-08-asset-system-design.md`

## Global Constraints

- Mobile scope is Android only; do not add iOS project files.
- QR scans are treated as strings; never navigate to the decoded URL from application code.
- Excel imports match by `asset_code`; blank source cells do not erase existing operational data.
- Asset state changes go through auditable asset events.
- YLF-280 printing is out of scope for this MVP; only keep an extension boundary.
- Do not add DingTalk credentials or production secrets to the repository.
- Use the supplied workbook only as local seed input; do not copy it into the repository.

---

### Task 1: Scaffold the runnable project

**Files:**
- Create: `package.json`
- Create: `vite.config.ts`
- Create: `tsconfig.json`
- Create: `tsconfig.node.json`
- Create: `index.html`
- Create: `server/index.ts`
- Create: `web/src/main.ts`
- Create: `web/src/App.vue`
- Create: `web/src/styles.css`
- Create: `.gitignore`
- Test: `server/health.test.ts`

**Interfaces:**
- Produces `npm run dev` for the API and Vite frontend, `npm test` for Node/Vitest tests, and `npm run build` for a production build.
- `server/index.ts` exports `createApp()` so API tests can exercise the same Express app without starting a network listener.

- [ ] **Step 1: Write the failing health test**

```ts
import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { createApp } from './index';

describe('health endpoint', () => {
  it('reports the service as ready', async () => {
    const response = await request(createApp()).get('/api/health');
    expect(response.status).toBe(200);
    expect(response.body).toEqual({ ok: true });
  });
});
```

- [ ] **Step 2: Run the test and verify the expected missing-module failure**

Run: `npm test -- server/health.test.ts`

Expected: FAIL because the project files and `createApp` do not exist yet.

- [ ] **Step 3: Add the package scripts and minimal app**

Add dependencies for Express, CORS, Multer, `xlsx`, Vue, Vite, TypeScript, Vitest, Supertest and `tsx`. Implement `createApp()` with `/api/health`; configure the Vite proxy from `/api` to the API port during development.

- [ ] **Step 4: Run the test and build**

Run: `npm test -- server/health.test.ts` and `npm run build`

Expected: the health test passes and Vite produces `dist/`.

- [ ] **Step 5: Commit the scaffold**

```bash
git add package.json package-lock.json vite.config.ts tsconfig.json tsconfig.node.json index.html server web .gitignore
git commit -m "chore: scaffold asset system mvp"
```

### Task 2: Implement QR parsing and SQLite schema with tests first

**Files:**
- Create: `shared/qr.ts`
- Create: `server/db.ts`
- Create: `server/migrations.ts`
- Create: `shared/qr.test.ts`
- Create: `server/db.test.ts`
- Modify: `server/index.ts`

**Interfaces:**
- `parseQr(raw: string): { raw: string; assetCode: string | null; format: 'gongbeiyun_url' | 'native_url' | 'asset_code' | 'unknown' }`, exported from `shared/qr.ts` and consumed by both server and web.
- `openDatabase(path?: string): DatabaseSync` creates `assets`, `asset_events`, `import_batches`, and `employees` tables and indexes.
- `createApp({ db }?: { db?: DatabaseSync })` accepts an injected database for tests.

- [ ] **Step 1: Write failing parser tests**

Cover the existing URL, a native URL, a plain code, whitespace, and an unrecognized string. Assert the raw value is preserved and the URL is never opened.

- [ ] **Step 2: Run parser tests and verify they fail**

Run: `npm test -- shared/qr.test.ts`

Expected: FAIL because `parseQr` is not defined.

- [ ] **Step 3: Implement the minimal parser**

Trim input, parse URLs with `URL`, extract the last non-empty path segment, recognize the configured Gongbeiyun host/path, recognize `^[A-Z]{2,5}-\d{3,6}$`, and return `unknown` for other values.

- [ ] **Step 4: Run parser tests and verify they pass**

Run: `npm test -- shared/qr.test.ts`

Expected: PASS.

- [ ] **Step 5: Write the failing schema smoke test**

Open an in-memory database and assert the tables exist and `asset_code` is unique.

- [ ] **Step 6: Run the schema test and verify it fails**

Run: `npm test -- server/db.test.ts`

Expected: FAIL because the schema module is not implemented.

- [ ] **Step 7: Implement migrations and repository-safe database setup**

Use `node:sqlite` `DatabaseSync`, parameterized statements, WAL mode, and foreign keys. Store asset fields required by the supplied workbook plus `qr_raw`, `qr_format`, timestamps, and current assignment fields. Keep event rows append-only. Import `parseQr` from `shared/qr.ts` in server code rather than duplicating QR rules.

- [ ] **Step 8: Run the schema tests and commit**

Run: `npm test -- shared/qr.test.ts server/db.test.ts`

Expected: PASS.

```bash
git add shared/qr.ts shared/qr.test.ts server/db.ts server/db.test.ts server/migrations.ts server/index.ts
git commit -m "feat: add qr parsing and asset database schema"
```

### Task 3: Build the Excel import pipeline

**Files:**
- Create: `server/importer.ts`
- Create: `server/importer.test.ts`
- Create: `server/routes/imports.ts`
- Modify: `server/index.ts`
- Modify: `server/db.ts`

**Interfaces:**
- `parseWorkbook(buffer: Buffer): ImportRow[]` maps the 22 supplied Chinese headers to stable field names.
- `previewImport(db, rows): ImportPreview` returns `newRows`, `updatedRows`, `unchangedRows`, `invalidRows`.
- `applyImport(db, rows, batchId): ImportResult` upserts by `asset_code` without blank-value erasure and records the batch.
- `POST /api/imports/preview` accepts a multipart `file` and returns a preview.
- `POST /api/imports/confirm` accepts the preview rows and applies a batch.

- [ ] **Step 1: Add fixture rows and failing import tests**

Test that the workbook headers map `资产编码`, `资产分类(一级)`, `资产状态`, `所在位置`, `使用人`, `使用部门`, and the three amount columns. Test a new row, an update row, duplicate codes, and that an empty source field does not erase a stored remark.

- [ ] **Step 2: Run the tests and verify they fail**

Run: `npm test -- server/importer.test.ts`

Expected: FAIL because the parser and upsert functions are missing.

- [ ] **Step 3: Implement workbook parsing and validation**

Use the `xlsx` package in raw-value mode, normalize dates to ISO strings, normalize numeric amounts, require a non-empty unique asset code, and collect row-level errors instead of throwing away the whole file.

- [ ] **Step 4: Implement preview and transactional upsert**

Compare incoming values to the stored row, classify changes, update only non-empty source values, preserve QR fields, photos, and events, and insert an `import_batches` record with counts and serialized errors.

- [ ] **Step 5: Run the tests and API route checks**

Run: `npm test -- server/importer.test.ts server/health.test.ts`

Expected: PASS; preview returns correct counts and confirm is idempotent for the same asset codes.

- [ ] **Step 6: Commit the importer**

```bash
git add server/importer.ts server/importer.test.ts server/routes/imports.ts server/db.ts server/index.ts
git commit -m "feat: import asset workbook safely"
```

### Task 4: Add asset query, statistics, detail, and event APIs

**Files:**
- Create: `server/repositories/assets.ts`
- Create: `server/routes/assets.ts`
- Create: `server/routes/events.ts`
- Create: `server/routes/stats.ts`
- Create: `server/assets.test.ts`
- Modify: `server/index.ts`

**Interfaces:**
- `GET /api/assets?search=&status=&category=&location=&page=&pageSize=` returns `{ items, total, page, pageSize }`.
- `GET /api/assets/:assetCode` returns the asset and its event timeline.
- `POST /api/assets/:assetCode/events` accepts `{ type, operatorName, assignee, department, location, note }` and returns the updated asset and event.
- `GET /api/stats` returns total, status counts, category counts, location counts, company counts, and total amounts.

- [ ] **Step 1: Write failing API tests**

Seed two assets in an in-memory database. Test filtering, pagination, QR lookup through the asset code, a `领用` event changing status to `在用`, a `归还` event changing status to `空闲`, and event history ordering.

- [ ] **Step 2: Run the tests and verify they fail**

Run: `npm test -- server/assets.test.ts`

Expected: FAIL because the routes and repository functions do not exist.

- [ ] **Step 3: Implement repository queries and event transitions**

Use parameterized SQL and a transaction for each event. Enforce valid event types, return a 409 for a conflicting current state, and make the event idempotent when the same request id is retried.

- [ ] **Step 4: Implement stats queries and mount routes**

Keep statistics derived from current asset rows; event tables are used for audit timelines, not for dashboard double-counting.

- [ ] **Step 5: Run API tests and commit**

Run: `npm test -- server/assets.test.ts server/importer.test.ts server/qr.test.ts server/db.test.ts`

Expected: all API and domain tests pass.

```bash
git add server/repositories server/routes server/assets.test.ts server/index.ts
git commit -m "feat: add asset queries events and dashboard stats api"
```

### Task 5: Build the Web dashboard and asset list

**Files:**
- Create: `web/src/api.ts`
- Create: `web/src/types.ts`
- Create: `web/src/components/SidebarNav.vue`
- Create: `web/src/components/MetricCard.vue`
- Create: `web/src/components/AssetTable.vue`
- Create: `web/src/components/AssetDetailDrawer.vue`
- Modify: `web/src/App.vue`
- Modify: `web/src/styles.css`

**Interfaces:**
- `api.getStats(): Promise<Stats>`
- `api.listAssets(params): Promise<AssetListResponse>`
- `api.getAsset(assetCode): Promise<AssetDetail>`
- `api.createEvent(assetCode, input): Promise<AssetDetail>`

- [ ] **Step 1: Add frontend type and API contracts**

Define stable types matching the API response shapes and a single fetch wrapper that displays server errors without leaking stack traces.

- [ ] **Step 2: Build the dashboard shell**

Create the left navigation and main content area matching the reference layout: blue header, light-gray page background, metric cards, status bars, category/location/company summaries, and a compact recent-assets table.

- [ ] **Step 3: Build the asset list view**

Add search by code/name/model/serial number, status/category/location filters, pagination, column labels in Chinese, and a detail drawer opened by clicking an asset code.

- [ ] **Step 4: Build the detail and event actions**

Show QR raw string, parsed asset code, current status, current assignee, department, location, model, serial number, amount, and event timeline. Add confirmation forms for 领用 and 归还 that call the event API and refresh the detail/list.

- [ ] **Step 5: Run the build and browser smoke check**

Run: `npm run build`; start the server with the seeded database and verify the dashboard, list filters, detail drawer, and event action in a browser.

- [ ] **Step 6: Commit the web vertical slice**

```bash
git add web package.json package-lock.json
git commit -m "feat: add asset dashboard list and detail views"
```

### Task 6: Add QR lookup and Android-ready responsive behavior

**Files:**
- Create: `web/src/components/QrLookup.vue`
- Create: `web/src/qr-lookup.test.ts`
- Create: `web/public/manifest.webmanifest`
- Create: `web/public/icon.svg`
- Modify: `web/src/App.vue`
- Modify: `web/src/styles.css`
- Modify: `package.json`

**Interfaces:**
- `QrLookup` emits `{ raw, assetCode }` after parsing a pasted or scanned string.
- The responsive route works in a narrow Android viewport without horizontal scrolling.

- [ ] **Step 1: Add failing parser integration test**

Exercise the shared parser with the exact `https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431` value and assert the UI lookup uses `CSJ-0431` rather than navigating to the URL.

- [ ] **Step 2: Implement the QR lookup panel**

Provide a camera button using `BarcodeDetector` when the browser exposes it, plus a paste/manual-input fallback. Always route decoded text through `parseQr`; show a clear error for unknown content.

- [ ] **Step 3: Add Android-friendly layout and install metadata**

Add a responsive bottom action area, large touch targets, safe loading/error states, and a web manifest. Add Capacitor configuration and scripts only when the Android wrapper is introduced; do not generate iOS files.

- [ ] **Step 4: Run viewport smoke checks and commit**

Run: `npm run build`; verify at 360px and 412px widths that QR lookup, asset detail, and event actions remain usable.

```bash
git add web package.json
git commit -m "feat: add qr lookup and android responsive shell"
```

### Task 7: Add the Android wrapper boundary

**Files:**
- Create: `capacitor.config.ts`
- Modify: `package.json`
- Modify: `.gitignore`

**Interfaces:**
- `npm run android:sync` builds the web app and syncs it into the Android Capacitor project.
- `npm run android:open` opens the generated Android project when the local Android SDK is installed.

- [ ] **Step 1: Write the wrapper configuration check**

Add a Node test that imports the Capacitor configuration and asserts the app id is `com.asset.local`, the web directory is `dist`, and no iOS platform is configured.

- [ ] **Step 2: Run the configuration test and verify it fails**

Run: `npm test -- capacitor.config.test.ts`

Expected: FAIL because Capacitor is not configured.

- [ ] **Step 3: Add the Capacitor Android configuration**

Install `@capacitor/core`, `@capacitor/cli`, `@capacitor/android`, and `@capacitor/camera`. Add scripts for build, sync, and open. Run `npx cap add android` once to create the Android project; do not run any iOS command.

- [ ] **Step 4: Run web build and wrapper checks**

Run: `npm run build`, `npm run android:sync`, and `npm test -- capacitor.config.test.ts`.

Expected: the web bundle syncs to the Android project. If the local Android SDK is absent, record that `android:open` requires Android Studio/SDK while keeping the generated project and sync script usable.

- [ ] **Step 5: Commit the Android wrapper boundary**

```bash
git add capacitor.config.ts capacitor.config.test.ts package.json package-lock.json .gitignore android
git commit -m "feat: add android capacitor shell"
```

### Task 8: Seed the supplied workbook and verify the acceptance slice

**Files:**
- Create: `scripts/import-workbook.ts`
- Create: `scripts/verify-seed.ts`
- Create: `README.md`
- Modify: `.gitignore`

**Interfaces:**
- `npm run import -- "C:\\Users\\张洋华\\Downloads\\资产清单2026-10-08.xlsx"` imports a workbook into `.data/asset.db`.
- `npm run verify:seed` prints and asserts total 1,762, 在用 1,249, 空闲 513, and unique asset code count 1,762 for the supplied snapshot.

- [ ] **Step 1: Write the failing seed verification script**

The script opens the database and asserts the four expected counts; it should fail before the workbook is imported.

- [ ] **Step 2: Implement the import CLI and seed verification**

Reuse the same importer as the HTTP route, create the `.data` directory when needed, and keep the workbook path as a command-line argument.

- [ ] **Step 3: Import and verify the real workbook**

Run:

```bash
npm run import -- "C:\Users\张洋华\Downloads\资产清单2026-10-08.xlsx"
npm run verify:seed
```

Expected: the counts match the workbook snapshot and `CSJ-0431` is queryable.

- [ ] **Step 4: Run the complete verification suite**

Run: `npm test` and `npm run build`.

Expected: all tests pass, the production build succeeds, and no `.data` or workbook file is tracked by Git.

- [ ] **Step 5: Commit the runnable MVP**

```bash
git add scripts README.md .gitignore package.json package-lock.json
git commit -m "feat: deliver runnable asset system mvp"
```

## Completion Checklist

- [ ] Design requirements have a corresponding implementation task.
- [ ] QR parsing never navigates to the vendor URL.
- [ ] Excel import is idempotent and preserves operational fields.
- [ ] Dashboard, filtered list, detail, QR lookup, and event transitions are usable in a browser.
- [ ] Android responsive behavior is verified; native Android packaging remains a subsequent build step.
- [ ] The supplied workbook is imported and the seed counts are verified.
- [ ] Printing and iOS remain outside the MVP.
