# hub-bff

Уеб backend-for-frontend (BFF) за hub-а на AO в Railway, по решението от
[ADR 0006](../../docs/adr/0006-web-bff-for-hub.md). Това е отделен Node процес,
а не listener на daemon-а. Той е единствената публична повърхност на hub-а:
сервира уеб build-а на renderer-а, държи login-а на единствения оператор и (от
RW-2.4.2) проксира към daemon-ите през автентикираните им listener-и. Никога не
се свързва с loopback listener-а на daemon-а на `127.0.0.1:3001`.

Този пакет е скелетът от RW-2.4.1: статично SPA, login, session cookie, CSRF и
ограничаване на опитите за login. Префиксите `/api/*`, `/mux`, `/hosts/*` и
`/readyz` са запазени за proxy-то и до RW-2.4.2 отговарят с `501` на влязъл
потребител.

## Пускане

Node 24 изпълнява TypeScript изходниците директно (type stripping), така че няма
build стъпка.

```bash
npm --prefix frontend run build:web                    # → frontend/dist-web
printf '%s' 'the password' | npm --prefix packages/hub-bff run --silent hash-password
AO_HUB_SPA_DIR=frontend/dist-web AO_WEB_PASSWORD_HASH='$scrypt$...' PORT=8080 \
  npm --prefix packages/hub-bff start
npm --prefix packages/hub-bff test                     # vitest + supertest
npm --prefix packages/hub-bff run typecheck
```

| Променлива | Задължителна | Значение |
|---|---|---|
| `AO_HUB_SPA_DIR` | да | Директорията на уеб build-а (`frontend/dist-web`); трябва да съдържа `index.html`. |
| `AO_WEB_PASSWORD_HASH` | една от двете | Хеш на паролата на оператора от `npm run hash-password`. Предпочитан вариант. |
| `AO_WEB_PASSWORD` | една от двете | Паролата на оператора в чист текст. Хешира се при старт, маха се от env-а на процеса и никога не се логва. |
| `PORT` | не | Порт за слушане. Railway го задава. По подразбиране `8080`. |
| `AO_HUB_BFF_HOST` | не | Адрес за слушане. По подразбиране `::` (всички интерфейси, dual-stack), което Railway изисква. |
| `AO_WEB_ORIGIN` | не | Публичният origin, например `https://hub.example.com`. Ако е зададен, заявките, които променят състояние, трябва да носят точно този `Origin`; иначе `Origin` трябва да съвпада с `Host` на заявката. |

Смяната на паролата е смяна на променливата и redeploy. Сесиите са в паметта,
затова redeploy излогва и всички потребители.

## Маршрути

| Маршрут | Auth | Поведение |
|---|---|---|
| `GET /healthz` | няма | `200 {"ok":true}`. Фиксирано тяло, не пипа daemon. |
| `GET /login`, `GET /_bff/login.{css,js}` | няма | Минимална login страница, която BFF-ът сервира сам, със строг CSP. Пренасочва към `/`, ако потребителят вече е влязъл. |
| `POST /login` | няма | JSON `{"password": "..."}`. `204` + session cookie, `401` при грешна парола, `429` + `Retry-After` при надвишен лимит. |
| `POST /logout` | няма | Прекратява сесията и изчиства cookie-то. `204`. |
| всичко останало | сесия | SPA-то от `AO_HUB_SPA_DIR`, с `index.html` като резервен отговор за клиентски маршрути без разширение. Без сесия: `302 /login` при навигация на страница, иначе `401 {"error":"unauthorized"}`. |

Login страницата е на BFF-а, а не маршрут в SPA-то: по ADR 0006 самата обвивка на
SPA-то е зад сесията, така че SPA-то не може да покаже login форма преди вход.
SPA-то трябва само да третира `401` от BFF-а като „отиди на `/login`“.

## Модел на сигурност

- **Session cookie** `ao_hub_session`: случаен 256-битов id, `HttpOnly; Secure;
  SameSite=Strict; Path=/`. Сесиите се пазят в паметта с плъзгащ се idle timeout
  от 7 дни и абсолютен живот от 30 дни (`Max-Age` на cookie-то). Няма секрет за
  подписване: id-то е непрозрачно и има смисъл само за този процес.
- **CSRF**: освен `SameSite=Strict`, всяка заявка, различна от `GET`/`HEAD`,
  включително `POST /login`, трябва да носи `Origin`, който съвпада с host-а (или
  с `AO_WEB_ORIGIN`), а `Sec-Fetch-Site`, ако е изпратен, трябва да е
  `same-origin`. Иначе `403`. `POST /login` изисква и
  `Content-Type: application/json`, който cross-site форма не може да изпрати.
- **Ограничаване на login**: 5 опита на минута на клиент. Клиентът е
  най-десният валиден запис в `X-Forwarded-For`, същото правило като в
  [ADR 0005](../../docs/adr/0005-trusted-proxy-remote-host.md), защото BFF-ът
  стои зад edge-а на Railway. Ако най-десният запис не е валиден IP, се ползва
  TCP peer-ът, никога по-ранен запис. Ключът служи само за ограничаване.
  Пускай BFF-а само зад един доверен proxy: клиент, който достига порта
  директно, може сам да избере ключа си.
- **Логове**: по един JSON обект на ред в stdout. Редовете за заявки съдържат
  метода, пътя без query string, статуса и продължителността. Редовете за login
  съдържат ключа на клиента. Пароли, хешове, cookies и headers никога не се
  логват.

## Решения

### Без framework (`node:http`)

BFF-ът е върху `node:http` без runtime зависимости, вместо `fastify` +
`@fastify/http-proxy`:

- Той е единствената публична повърхност на hub-а, така че всяка зависимост е
  повърхност за атака и supply chain риск в процеса, който пази shell на всеки
  хост. Без runtime зависимости това се свежда до самия Node.
- Наборът маршрути е малък (health, login, logout, статични файлове и
  префиксите на proxy-то), така че router дава малко.
- Proxy-то от RW-2.4.2 има работещ прецедент в репото върху `node:http`:
  `frontend/src/main/remote-proxy.ts` вече препраща REST, стриймва SSE и
  свързва WebSocket upgrade-а на `/mux` с инжектиран bearer. Пренасянето на тази
  логика е по-малък риск от нагаждането на обработката на headers в
  `@fastify/http-proxy` към правилата на ADR 0006 за махане и инжектиране.

Зависимостите само за разработка (`typescript`, `vitest`, `supertest`,
`@types/*`) са пинати на точни версии.

### Хеширане на паролата: scrypt от `node:crypto`

ADR 0006 допуска „salted scrypt или argon2id хеш“. Сравнени варианти:

| Вариант | Решение |
|---|---|
| `argon2` (npm) | Native addon (node-gyp или prebuilt binaries). Добавя native build в Railway image-а и supply chain зависимост за единствената проверка на секрет. Отхвърлен. |
| `bcryptjs` | Чист JS, но bcrypt реже паролите на 72 байта, не е memory-hard и не е от двете схеми в ADR-а. Отхвърлен. |
| `argon2` от `node:crypto` | Има го в последните версии на Node 24, но още е експериментален. Не разчитаме на него. |
| `scrypt` от `node:crypto` | Вграден, memory-hard, без зависимост. **Избран.** |

Параметрите са базовите на OWASP за scrypt, `N=2^17, r=8, p=1` (128 MiB и малко
под секунда на проверка на машина за разработка, което забавя и налучкването).
Хешът се пази като `$scrypt$ln=17,r=8,p=1$<salt>$<hash>` (base64url без
padding), така че цената може да се вдигне по-късно, без да се чупят старите
хешове. Сравнението е с `timingSafeEqual`.
