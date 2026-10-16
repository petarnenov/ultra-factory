# RW-2.3.2 · Таск: Web Notifications и badge

**Тип:** frontend TS. **Оценка:** 0.5 чд. **Стори:** RW-2.3.

## Контекст
- `notifications` група (6 метода): OS notification, badge, sound. `main/notification-signals.ts`. Stub-ът в `bridge.ts` е no-op.

## Събтаскове
- [ ] В browser stub: `notifications.show` → `new Notification(...)` след `Notification.requestPermission()` при първо взаимодействие; `setBadge(n)` → префикс `(n) ` в `document.title`; sound → `<audio>` с бъндълнат звук, ако Electron ползва такъв.
- [ ] Permission prompt се показва от Settings → Notifications бутон, не автоматично при load.
- [ ] Тест с mock на `Notification`.
