# RW-3.3.2 · Таск: Диалог „Добави работник“

**Тип:** frontend React. **Оценка:** 0.5–1 чд. **Стори:** RW-3.3.

## Събтаскове
- [ ] Два таба: „Create in Railway“ (label, размер/region ако provider-ът поддържа, Claude auth: API key или OAuth token, GitHub token, git name/email) и „Add existing by address“ (label, URL, парола → RW-2.4.3 API).
- [ ] Валидация: label `[a-z0-9-]{3,32}` уникален; поне една Claude credential; `GH_TOKEN` формат.
- [ ] Submit → `POST /hub/workers` → затваря диалога, списъкът показва provisioning. Грешки 429 (лимит) и 5xx с четимо съобщение.
- [ ] Полетата за секрети са `type=password`, без autocomplete, изчистват се при затваряне.
- [ ] Vitest: валидация и submit payload.
