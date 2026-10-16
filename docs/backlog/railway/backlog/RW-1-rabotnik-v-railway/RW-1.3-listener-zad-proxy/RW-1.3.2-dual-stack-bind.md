# RW-1.3.2 · Таск: Dual-stack bind на remote-host listener-а

**Тип:** backend Go. **Оценка:** 0.5–1 чд. **Стори:** RW-1.3. **Зависимости:** RW-1.1.3.

## Цел
Listener-ът да може да слуша на `[::]` (IPv4+IPv6), за да е достъпен по Railway private networking.

## Контекст
- `backend/internal/httpd/lan_listener.go:201-204` `Start` → `m.start(port, "0.0.0.0")`.
- `backend/internal/mobilebridge/netiface.go` изброява адресите за показване при pairing.
- Решението от RW-1.1.3 определя дали е opt-in (`AO_REMOTE_BIND=dual`) или default.

## Събтаскове
- [ ] Config: `AO_REMOTE_BIND` ∈ {`ipv4` (default), `dual`}; валидация и тест в `config_test.go`.
- [ ] `LANManager.Start` избира host `"0.0.0.0"` или `"::"` според конфигурацията. `StartLoopback` остава `127.0.0.1`.
- [ ] Увери се, че `net.JoinHostPort` навсякъде, където се печата адресът (`remote_host.go:84`, status отговори), работи с IPv6 (квадратни скоби).
- [ ] `netiface.go`: при dual включи и глобалните IPv6 адреси в списъка, или остави само IPv4 за pairing UI-я, според ADR-а.
- [ ] Тест: listener на `::` приема връзка и по `127.0.0.1` и по `::1`.

## Критерии за приемане
- [ ] `AO_REMOTE_BIND=dual ao daemon` + enable → `curl http://127.0.0.1:3011/api/v1/identity` и `curl 'http://[::1]:3011/api/v1/identity'` → 200.
- [ ] Без env → поведение като днес.
