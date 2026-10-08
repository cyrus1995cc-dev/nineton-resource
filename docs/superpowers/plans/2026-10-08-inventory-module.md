# Asset inventory module Implementation Plan

> **For agentic workers:** Execute inline in this session with test-first checkpoints.

**Goal:** Add a connected inventory workflow to the existing asset system so staff can create scoped tasks, scan assets, record differences, and close a task.

**Architecture:** Materialize the asset scope into `inventory_records` when an `inventory_task` is created. This preserves the expected state at task start and makes missing, location, and status differences auditable even after assets change. REST endpoints sit beside the existing asset/event routes and the Vue app gets a focused inventory page that reuses QR parsing and asset detail lookup.

**Tech Stack:** Node.js `node:sqlite`, Express, TypeScript, Vitest/Supertest, Vue 3 + Vite.

**Spec:** `docs/superpowers/specs/2026-10-08-asset-system-design.md`

## Global Constraints

- QR contents remain strings and are never opened as URLs.
- Existing asset codes remain immutable.
- Existing role rules apply: viewers can read; asset managers/admins can write.
- The first release is online and uses the current SQLite database.

### Task 1: Inventory persistence and API

**Files:**
- Modify: `server/migrations.ts`, `server/db.test.ts`
- Create: `server/repositories/inventory.ts`, `server/routes/inventory.ts`, `server/inventory.test.ts`
- Modify: `server/index.ts`

**Interfaces:**
- `POST /api/inventory/tasks` creates a task and materializes matching assets.
- `GET /api/inventory/tasks` lists recent tasks.
- `GET /api/inventory/tasks/:id` returns task plus records.
- `POST /api/inventory/tasks/:id/records` records `found` or calculates `mismatch`.
- `POST /api/inventory/tasks/:id/complete` marks pending records `missing` and closes the task.

### Task 2: Web inventory workflow

**Files:**
- Modify: `web/src/types.ts`, `web/src/api.ts`, `web/src/App.vue`, `web/src/components/SidebarNav.vue`, `web/src/styles.css`
- Create: `web/src/components/InventoryPanel.vue`

**Interfaces:**
- Inventory page can create a task by location, choose an active task, submit QR text or asset code, and close the task.
- All scans pass through the existing `parseQr` helper and never navigate to a decoded URL.

### Task 3: Verification and Android sync

**Files:**
- Modify: `android/gradle.properties`

**Checks:** `npm test`, `npx tsc --noEmit`, `npm run build`, `npm run android:sync`, `gradle assembleDebug`.
