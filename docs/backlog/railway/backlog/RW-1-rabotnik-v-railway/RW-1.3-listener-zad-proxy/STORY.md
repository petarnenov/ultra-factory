# RW-1.3 · Стори: Remote-host listener зад reverse proxy

**Като** оператор **искам** remote-host listener-ът да работи правилно зад Railway edge и по IPv6 private networking, **за да** не се блокират всички клиенти при един грешен опит и да е достъпен за hub-а.

**Оценка:** 2–3 чд. **Зависимости:** RW-1.1.1, RW-1.1.3. **Блокира:** RW-1.4 (частично), RW-3.

| Таск | Име | чд |
|---|---|---|
| RW-1.3.1 | Trusted-proxy режим за lockout | 1 |
| RW-1.3.2 | Dual-stack bind на listener-а | 0.5–1 |
| RW-1.3.3 | Expected-host-ID guard и identity probe през proxy | 0.5 |

**Критерии за приемане**
- [ ] 5 грешни пароли от клиент A не блокират клиент B зад същия proxy, когда trusted-proxy режимът е включен.
- [ ] `curl -6 http://[<v6>]:3011/api/v1/identity` → 200 при включен dual-stack.
- [ ] Поведението без env променливи е идентично с днешното (LAN потребители).
