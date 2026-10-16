# RW-4.1.1 · Таск: Threat model и security headers

**Тип:** security + Node TS. **Оценка:** 0.5–1 чд. **Стори:** RW-4.1.

## Събтаскове
- [ ] `docs/railway/threat-model.md`: активи (Railway токен, host credentials, агентски ключове, кодът в worktrees), нападатели, вектори, митигации, остатъчен риск. Изрично: „достъп до UI = shell на всички работници“.
- [ ] BFF headers: `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, `Content-Security-Policy` съгласуван с RW-2.1.2 (BFF-ът сервира SPA-то, така че CSP може да е header, не meta).
- [ ] Session: invalidate при смяна на operator password; `GET /hub/sessions` + `DELETE` за logout навсякъде.
- [ ] Проверка, че 401 от работник не изтича hostId/URL към неавтентикиран клиент.
- [ ] Тестове за headers.
