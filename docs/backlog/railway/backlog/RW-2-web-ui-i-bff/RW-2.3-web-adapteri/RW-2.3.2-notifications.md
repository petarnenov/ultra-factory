# RW-2.3.2 · Task: Web Notifications and badge
**Status:** ✅ done (#76)

**Type:** frontend TS. **Estimate:** 0.5 pd. **Story:** RW-2.3.

## Context
- The `notifications` group (6 methods): OS notification, badge, sound. `main/notification-signals.ts`. The stub in `bridge.ts` is a no-op.

## Subtasks
- [ ] In the browser stub: `notifications.show` → `new Notification(...)` after `Notification.requestPermission()` on first interaction; `setBadge(n)` → a `(n) ` prefix in `document.title`; sound → `<audio>` with a bundled sound, if Electron uses one.
- [ ] The permission prompt is shown from a Settings → Notifications button, not automatically on load.
- [ ] Test with a `Notification` mock.
