# WM-1.2.2 · Task: Settings tools

**Status:** ⬜ todo — [#474](https://github.com/petarnenov/ultra-factory/issues/474)

**Type:** frontend. **Estimate:** 0.5–1 pd. **Story:** WM-1.2.

## Goal

Register WebMCP tools while the global settings overlay is open that read the
section list and update state, select a section, and trigger the update
actions the selected scenarios exercise.

## Context (verified)

- `/#/settings` is **not a page**: `routes/_shell.settings.tsx:11-21` calls
  `useUiStore.openGlobalSettings()` then navigates to `/`. Settings is an
  overlay driven by `settingsModal` (`stores/ui-store.ts:202,429`).
- The active section is local state in `useSettingsLayer`
  (`components/SettingsDialog.tsx:35`, `useState<GlobalSettingsSection>("general")`
  at line 113). Selecting: `selectGlobalSection(id)` (lines 228-231).
  `navItems` (lines 232-234) are `{id,label,icon,active,onSelect}`, exposed
  via `useSettingsPage()` (line 301). The pane is `SettingsPane` (line 387,
  `data-testid="settings-page"`), mounted at `_shell.tsx:142`.
- Section ids and visibility: `components/settings/settingsCatalog.tsx:64-171`
  (`general, harness, agents, billing, browserProfiles, coder, remoteHosts,
  workers, mobile, shortcuts, diagnostics, updates, help`). Rendered sections
  carry `[data-testid="settings-section"][data-section=<id>]`
  (`settings/SettingsSection.tsx:25-26`).
- Updates section (`components/settings/UpdatesSection.tsx`): download
  `requestUpdateDownload` (`hooks/useUpdateStatus.ts:92`, used at 503),
  install `useRequestUpdateInstall()` (`hooks/useRequestUpdateInstall.ts:9`,
  used at 513), check `checkNow` (line 440, `aoBridge.updates.check`), status
  `useUpdateStatus` (`useUpdateStatus.ts:65`). Test ids `app-version` (470),
  `update-status-line` (546).
- e2e drives update state through `window.__aoFakeUpdates.setStatus(...)`
  (`e2e/support/fake-bridge.ts:376-379`); the catalog models it as an
  external bridge step.

## Blueprint

```ts
// frontend/src/renderer/webmcp/tools/settings.ts
export type SettingsRead = {
  open: boolean;
  activeSection: GlobalSettingsSection | null;
  sections: { id: GlobalSettingsSection; label: string }[];   // from navItems
  updates: { state: string; version?: string; percent?: number; appVersion: string } | null;
};

export function settingsTools(ctx: {
  navItems: { id: GlobalSettingsSection; label: string; active: boolean; onSelect: () => void }[];
  updateStatus: UpdateStatus | undefined;           // useUpdateStatus()
  appVersion: string;
  download: () => Promise<void> | void;             // requestUpdateDownload
  install: () => Promise<void> | void;              // useRequestUpdateInstall()
  check: () => Promise<void> | void;                // checkNow
  subscribeUpdates: (cb: () => void) => () => void; // same listener useUpdateStatus uses
}): ToolDescriptor[] {
  return [
    { name: "settings.read", description: "Return settings sections, the active one, and update state.", ... },
    { name: "settings.select_section", description: "Activate a settings section by id.",
      inputSchema: { type: "object", required: ["id"], properties: { id: { type: "string", enum: ctx.navItems.map(n => n.id) } } },
      execute: ({ id }) => { const item = ctx.navItems.find(n => n.id === id); if (!item) throw new ToolError("precondition", id); item.onSelect(); return { activeSection: id }; } },
    { name: "settings.update.download", description: "Start downloading the available update.", ... },
    { name: "settings.update.install", description: "Install the downloaded update.", ... },
    { name: "settings.update.check", description: "Check for updates now.", ... },
    { name: "settings.wait_for_update_state",
      description: "Resolve when update state equals the given value, or fail after timeoutMs.",
      inputSchema: { ..., required: ["state"], properties: { state: { type: "string" }, timeoutMs: { type: "number" } } }, ... },
  ];
}
```

- Registration lives in `SettingsProvider` (`SettingsDialog.tsx:310`) so tools
  exist exactly while the overlay is open (`settingsModal.scope === "global"`),
  and are unregistered on close. `updates.*` descriptors are registered only
  when `updates` is in `navItems` (same gate as the catalog:
  `capabilities.autoUpdate`).
- `settings.read.updates.state` mirrors the values the fake bridge uses:
  `available | downloading | preparing | downloaded | ...` (see
  `e2e/updates-settings.spec.ts:49-84`).

## Subtasks

- [ ] `tools/settings.ts` with the six descriptors.
- [ ] Wire in `SettingsProvider`; pass `navItems`, update hooks, and the
  update-status subscription.
- [ ] Vitest: `select_section` calls the matching `onSelect`; unknown id
  throws `precondition`; `read` lists exactly the visible catalog ids.
- [ ] e2e `webmcp-settings.spec.ts` @P0: `installFakeBridge({updateStatus:{state:"available",version:"2.0.0"}})`,
  goto `/#/settings`, `callTool("settings.select_section",{id:"updates"})`,
  `callTool("settings.update.download")`, then
  `callTool("settings.wait_for_update_state",{state:"downloading"})`
  resolves; `listTools()` on `/#/` after close contains no `settings.*`.

## Acceptance criteria

- [ ] Tools present only while the global settings overlay is open.
- [ ] `settings.read.sections` equals the visible catalog; the ids used by
  SET-001 and the updates specs are a subset of that catalog.
- [ ] Every settings scenario step in the catalog maps to one tool or one
  bridge step.

## Verification

- `npm --prefix frontend run test:e2e -- e2e/smoke-t0.spec.ts --grep @SET`
- `npm --prefix frontend run test:e2e -- e2e/updates-settings.spec.ts e2e/webmcp-settings.spec.ts`
- `make ci-check`

## Dependencies

WM-1.1.1.

## Audit constraints — 2026-10-21

`SettingsProvider` remains mounted while closed and has separate project and
global layers. Registration must explicitly follow the global-open state, not
provider mount alone. Use current getters for section/update state; navigation
and React state commits are asynchronous. `useUpdateStatus` has a private
listener set, not an exported `subscribeUpdates` API: add a small supported
subscription boundary or reuse the hook without duplicating the bridge stream.
Preserve the update buttons' availability, pending and confirmation guards.
The close test must explicitly close settings; navigating to `/` alone does
not close a store-driven settings surface.
