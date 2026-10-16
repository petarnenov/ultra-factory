# RW-1.1.3 · Таск: ADR за dual-stack bind на remote-host listener-а

**Тип:** ADR. **Оценка:** 0.5 чд. **Стори:** RW-1.1.

## Цел
Решение дали `LANManager.Start` да bind-ва `[::]` (IPv4+IPv6) вместо `0.0.0.0`, за да е достъпен по IPv6 private networking.

## Контекст
- `backend/internal/httpd/lan_listener.go:202` — `m.start(port, "0.0.0.0")`.
- Railway private networking е (поне исторически) само IPv6, `*.railway.internal` резолвва до IPv6. Проверката е част от RW-1.4.
- Go `net.Listen("tcp", "[::]:3011")` на Linux е dual-stack по подразбиране (`net.ipv6.bindv6only=0`). На macOS също. Поведението за LAN потребители не се променя, освен че listener-ът става достъпен и по IPv6 в LAN.
- ADR `0001` описва LAN listener-а. Промяната на bind адреса му е в неговия обхват.

## Събтаскове
- [ ] Провери поведението на `bindv6only` в типичен Railway контейнер (Debian/trixie).
- [ ] Реши: винаги dual-stack или opt-in env `AO_REMOTE_BIND=dual`. Препоръка: opt-in за първия релийз, за да не се сменя поведението на LAN потребителите без предупреждение.
- [ ] Отбележи ефекта върху `netiface.go` (списък на адресите, които UI-ят показва за pairing).
- [ ] Напиши ADR или допълнение към `0001`.

## Критерии за приемане
- [ ] Решението е записано и RW-1.3.2 има ясна спецификация.
