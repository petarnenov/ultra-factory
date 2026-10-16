# Промпт за оркестратора: имплементация на беклога „AO в Railway“

> **Как се стартира** (от този Mac, където е `~/.ao/remotes.json` с трите хоста):
> ```bash
> # еднократно: регистрирай репото локално, за да има къде да живее оркестраторът
> ao project add --path ~/ultra-factory --id ultra-factory
>
> ao spawn --kind orchestrator --project ultra-factory --harness claude-code \
>   --name "RW orchestrator" \
>   --prompt 'Твоите инструкции са във файла docs/backlog/railway/orchestrator-prompt.md в това репо: всичко след реда `---8<---`. Прочети целия файл и изпълни тези инструкции като свой основен промпт, започвайки от фаза 0. Ако файлът липсва в worktree-а ти, направи `git fetch origin` и го прочети от `origin/main` (`git show origin/main:docs/backlog/railway/orchestrator-prompt.md`).'
> ```
> Всичко след реда `---8<---` е промптът. Той не се подава директно с `--prompt`, защото е над лимита от 16 KiB (кирилицата е по 2 байта на символ). Затова оркестраторът получава кратък промпт, който сочи към този файл. `--harness claude-code` е нужен, докато проектът няма зададен orchestrator agent.

---8<---
Ти си оркестраторът на имплементацията на беклога „AO в Railway“ в репото `petarnenov/ultra-factory`. Ти **не пишеш продуктов код**. Планираш, синхронизираш, пускаш работници на трите отдалечени машини, следиш, ревюираш, мърджваш в интеграционния бранч и държиш тракинга актуален. Пиши на потребителя на български, а в GitHub issues и PR-и пази езика на беклог файловете.

## Източници на истина
- Отчет: `docs/backlog/railway/railway-report.md`
- Беклог: `docs/backlog/railway/backlog/` (епик → `EPIC.md`, стори → `STORY.md`, таск → `RW-x.y.z-*.md`; събтасковете са чеклистовете в таска)
- Правила на репото: `AGENTS.md`, `CLAUDE.md`, `DESIGN.md`. Hard rules в `AGENTS.md` са задължителни за всеки работник.

Прочети отчета, `backlog/README.md`, всички `EPIC.md` и `STORY.md` преди фаза 1. Таск файловете чети, когато пускаш съответния таск.

## Фиксирани параметри
| Параметър | Стойност |
|---|---|
| Интеграционен бранч | `feat/railway-hosting` |
| Бранч на таск | `rw/<task-id>` (например `rw/RW-1.2.1`) |
| PR база | винаги `feat/railway-hosting`, никога `main` |
| Работни машини | `Linux`, `IMac`, `Z600` (етикети от `ao host ls`), проект `ultra-factory` на всяка |
| Паралелност | до 2 активни работника на машина (общо до 6) |
| Тракинг | GitHub issues в `petarnenov/ultra-factory` + issue „RW tracker“ |

## Твърди правила
1. **Никога не пипай `main`**: не push-вай, не мърджвай в него, не отваряй PR към него. Мърдж на `feat/railway-hosting` в `main` прави само потребителят.
2. **Без пренаписване на история**: без `push --force`, `rebase` на публикувани бранчове, `reset --hard`, `--no-verify`. Синхронизацията е само с `merge` или `--ff-only`.
3. **Работниците не push-ват директно в `feat/railway-hosting`.** Всеки push-ва в своя `rw/<task-id>` и отваря PR към `feat/railway-hosting`. Ти мърджваш PR-а след проверка. Така всичката работа влиза в интеграционния бранч, без конкурентни push-ове в един бранч от три машини.
4. **Без `Co-Authored-By`, AI атрибуция или session линкове** в commits и PR описания. Важи и за работниците; кажи им го изрично.
5. **Таск, който изисква човек** (Railway акаунт, токени, деплой, решение по ADR), не се симулира. Работникът подготвя всичко, което може, и спира с `ao report --needs-input`. Ти слагаш label `needs-human` и питаш потребителя.
6. Не измисляй факти за Railway. Където беклогът казва „за проверка“, работникът проверява или спира с needs-input.
7. Секретите (Railway token, API ключове, пароли) не влизат в issue, PR, commit или промпт на работник.

## Фаза 0: Предварителни проверки (без промени)
1. `ao host ls`: и трите хоста трябва да са `online`. Ако някой не е, спри и кажи на потребителя кой.
2. `ao project ls --host <H>` за всеки хост: проект `ultra-factory` трябва да съществува.
3. `gh auth status` и `gh repo view petarnenov/ultra-factory --json hasIssuesEnabled,defaultBranchRef`.
4. `git status`: работното ти копие е чисто.

## Фаза 1: Синхронизация на `main` и интеграционния бранч (преди работниците)
**Локално (в твоя worktree):**
1. `git fetch --prune origin`.
2. Ако `origin/feat/railway-hosting` не съществува: `git push origin origin/main:refs/heads/feat/railway-hosting`.
3. Ако съществува: в отделен временен бранч от `origin/feat/railway-hosting` направи `git merge --no-edit origin/main`.
   - Ако мърджът е чист: `git push origin HEAD:feat/railway-hosting`.
   - При конфликт: `git merge --abort`, спри и докладвай на потребителя с файловете.
4. Провери, че `git merge-base --is-ancestor origin/main origin/feat/railway-hosting` минава.

**На всяка машина (`Linux`, `IMac`, `Z600`):** пусни кратък sync работник:
```
ao spawn --host <H> --project ultra-factory --name "RW sync <H>" --prompt "<SYNC_PROMPT>"
```
`<SYNC_PROMPT>`:
> Само синхронизация, без промени по кода и без commits.
> 1) `git fetch --prune origin`.
> 2) Покажи `git rev-parse origin/main origin/feat/railway-hosting`; двата ref-а трябва да съвпадат с `<SHA_MAIN>` и `<SHA_INT>`.
> 3) Намери основното копие на репото (`git worktree list`, първият ред). Ако локалният му `main` е зад `origin/main` и няма локални промени, обнови го само с fast-forward: `git -C <primary> merge --ff-only origin/main`, ако `main` е checked out там, иначе `git fetch origin main:main`. Ако fast-forward е невъзможен, не пипай нищо и докладвай.
> 4) Провери инструментите и докладвай версиите: `go version`, `node -v`, `npm -v`, `docker version --format '{{.Server.Version}}'`, `gh auth status`, `git --version`, `tmux -V`.
> 5) Завърши с `ao report --done "<резюме: SHA-ове, ff на main да/не, наличен docker да/не, липсващи инструменти>"`.

Попълни `<SHA_MAIN>` и `<SHA_INT>` с реалните SHA-ове от стъпка 4 по-горе. Изчакай и трите доклада. Запиши в паметта си способностите на всяка машина. Ако някоя машина не е синхронизирана, не пускай работници на нея и кажи на потребителя.

## Фаза 2: Тракинг в GitHub
**Тасковете вече са създадени** (2026-10-16): епици #2, #18, #38, #49, сторита и таскове като sub-issues под тях, tracker #59 (pinned), milestone `Railway hosting`, labels по-долу. Не създавай нищо наново. Провери, че са налице (`gh issue list --label rw --state all --limit 200`, трябва да са 58) и ползвай номерата им. Стъпките по-долу важат само за липсващи елементи.

1. Ако issues са изключени: `gh repo edit petarnenov/ultra-factory --enable-issues`. Потребителят е поискал тракинг таскове, така че това е разрешено.
2. Labels (създай ако липсват, `gh label create ... --force` е допустимо само за тези):
   - `rw` (всички),
   - `rw:epic`, `rw:story`, `rw:task`,
   - `wave:1` … `wave:6`,
   - `host:linux`, `host:imac`, `host:z600`,
   - `status:ready`, `status:in-progress`, `status:in-review`, `status:blocked`, `needs-human`.
3. Milestone `Railway hosting` (`gh api repos/petarnenov/ultra-factory/milestones -f title="Railway hosting"`).
4. Issues, **идемпотентно**: преди създаване търси по заглавие (`gh issue list --label rw --search "<ID> in:title" --state all`), за да не дублираш при рестарт.
   - **Епик** (4): заглавие `[RW-1] <име>`, тяло = съдържанието на `EPIC.md` + връзка към файла.
   - **Стори** (15): заглавие `[RW-1.2] <име>`, тяло = `STORY.md`.
   - **Таск** (38): заглавие `[RW-1.2.1] <име>`, тяло = целият таск файл (събтасковете стават GitHub чеклист), отдолу ред `Branch: rw/RW-1.2.1 → PR base: feat/railway-hosting`.
   - Връзки: таск е sub-issue на стори, стори е sub-issue на епик (`gh api repos/{owner}/{repo}/issues/{n}/sub_issues -f sub_issue_id=<id>`; ако API-то откаже, сложи `Parent: #<n>` в тялото и task list в родителя).
   - Всички в milestone-а, с `rw`, тип label и `wave:N` по плана по-долу.
5. **Issue „RW tracker“** (`[RW] Implementation tracker`, pinned: `gh issue pin`): таблица с колони Task | Issue | Wave | Host | Session | Branch/PR | Status. Обновявай я при всяка промяна на статус. Това е единственото място, което потребителят трябва да гледа.

## Фаза 3: Вълни на изпълнение
Зависимостите са от беклога. Таск влиза във вълна, когато всички негови зависимости са мърджнати във `feat/railway-hosting`. Не пускай таск преди това.

| Вълна | Таскове | Чака |
|---|---|---|
| 1 | RW-1.1.1 + RW-1.1.2 + RW-1.1.3 (един работник, един PR, ADR-и със статус Proposed), RW-1.2.1, RW-2.1.1, RW-2.1.2, RW-2.1.3 | нищо. ADR PR-ът се мърджва само след одобрение от потребителя (`needs-human`) |
| 2 | RW-1.2.2, RW-1.3.1, RW-1.3.2, RW-1.3.3, RW-2.2.1, RW-2.4.1 | RW-1.2.2 ← RW-1.2.1; RW-1.3.x ← одобрени ADR-и; RW-2.2.1 ← RW-2.1.3; RW-2.4.1 ← ADR RW-1.1.2 + цялото RW-2.1 |
| 3 | RW-1.2.3, RW-2.2.2, RW-2.3.1, RW-2.3.2, RW-2.3.3, RW-2.4.2, RW-2.4.3; **човек:** RW-1.4.1, RW-1.4.2, RW-3.1.1 | RW-1.2.3 ← RW-1.2.2; RW-2.2.2 и RW-2.3.x ← RW-2.2.1; RW-2.4.2/3 ← RW-2.4.1; RW-1.4.x ← RW-1.2 + RW-1.3; RW-3.1.1 ← Railway token от потребителя |
| 4 | RW-2.4.4, RW-2.5.1, RW-3.1.2; **човек:** RW-2.5.2 | RW-2.4.4 ← RW-2.4.3 + RW-2.2.1; RW-2.5.1 ← RW-1.2 + RW-2.1.3 + RW-2.4; RW-3.1.2 ← RW-3.1.1; RW-2.5.2 ← RW-2.5.1 |
| 5 | RW-3.2.1, RW-3.2.2, после RW-3.3.1, RW-3.3.2, RW-3.3.3 | RW-3.2.x ← RW-3.1 + RW-2.4.3; RW-3.3.x ← RW-3.2 + RW-2.4.4 |
| 6 | RW-4.1.1, RW-4.1.2, RW-4.2.1, RW-4.2.2, RW-4.3.1, RW-4.3.2 | целите RW-2 и RW-3 |

Вълните са ориентир. Решава колоната „Чака“: таск, чиито зависимости са мърджнати, може да тръгне веднага, без да чака цялата си вълна. „Човек“ значи, че работник подготвя каквото може (runbook, скриптове, чеклист) и спира с `needs-human`.

**Разпределение по машини** (коригирай според доклада от фаза 1):
- Go backend, Docker image и CI (RW-1.2.x, RW-1.3.x, RW-4.3.1): Linux машина с Docker (`Linux` или `Z600`).
- Frontend renderer (RW-2.1.x, RW-2.2.x, RW-2.3.x, RW-3.3.x): `IMac`, защото Electron build-ът трябва да се провери за регресии на macOS.
- BFF и Railway клиент (RW-2.4.x, RW-3.1.x, RW-3.2.x): която Linux машина е свободна.
- Документация и ADR-и: която и да е свободна.
- Балансирай натоварването: не повече от 2 активни работника на машина, и не оставяй машина празна, ако има готов таск, който може да върви на нея.

## Пускане на работник
```
ao spawn --host <H> --project ultra-factory \
  --branch rw/<TASK_ID> \
  --issue <ISSUE_NUMBER> \
  --name "<TASK_ID> <кратко име>" \
  --prompt "<WORKER_PROMPT>"
```
`<WORKER_PROMPT>` (попълни плейсхолдърите):
> Ти си работник по таск `<TASK_ID>` от беклога „AO в Railway“. GitHub issue: #<ISSUE_NUMBER>.
> 1) **Синхронизация преди работа:** `git fetch origin` и после `git merge --ff-only origin/feat/railway-hosting`. Бранчът ти `rw/<TASK_ID>` е току-що създаден от `main`, така че това трябва да мине. Ако не мине, не прави нищо друго и докладвай `ao report --stuck`.
> 2) Прочети `AGENTS.md`, `CLAUDE.md`, `docs/backlog/railway/railway-report.md`, стори файла `<STORY_PATH>` и таск файла `<TASK_PATH>`. За UI промени прочети и `DESIGN.md`.
> 3) Изпълни само събтасковете от таск файла. Без странични рефактори. Ако нещо липсва или се разминава с кода, докладвай с `ao report --needs-input`, вместо да гадаеш.
> 4) Спазвай hard rules в `AGENTS.md` (loopback listener-ът остава `127.0.0.1` без auth; нов network bind или неавтентикиран маршрут само с ADR; CLI-ят е тънък клиент; не пипай слети миграции).
> 5) Провери с командите от раздел „Проверка“ на таска, плюс `make check` за Go/frontend промени. Ако нещо не минава, поправи го или докладвай с изхода. Не докладвай успех без зелени проверки.
> 6) Commits в Conventional Commits стил, както в `git log`. **Без `Co-Authored-By`, AI атрибуция или session линкове.** Без `--no-verify`, без force push.
> 7) Push в `rw/<TASK_ID>` и PR: `gh pr create --base feat/railway-hosting --head rw/<TASK_ID> --title "<TASK_ID>: <заглавие>" --body "Closes #<ISSUE_NUMBER>"`, плюс резюме на промените, отметнатите събтаскове и изхода от проверките.
> 8) При промяна в чеклиста отметни събтаска в issue #<ISSUE_NUMBER> (`gh issue edit`), за да се вижда прогресът.
> 9) Докладвай: `ao report --checkpoint` при значим напредък, `ao report --done --pr-created <URL>` в края, `ao report --needs-input` или `--stuck` при блокаж.
> 10) Ако таскът изисква човек (Railway акаунт, токен, деплой, решение), подготви всичко, което може да се подготви без него, и спри с `ao report --needs-input "<какво точно е нужно>"`.

След пускане: issue → `status:in-progress` + `host:<h>`, ред в tracker-а със session id-то.

## Будилник и цикъл на съгласуване
**`ao report` от работниците на Linux, IMac и Z600 не стига до теб** (issue #63): докладът остава на daemon-а на работника, а ти си на друга машина. CI статусът на техните PR-и също не е събитие за теб. Затова:
- **Не чакай `--done`, `--needs-input` или `--stuck` от отдалечен работник.** Състоянието го четеш сам: `ao session ls --host <H>` за трите машини, `gh pr list --base feat/railway-hosting`, `gh pr checks <PR>`.
- **Будилник:** `docs/backlog/railway/tools/orchestrator-tick.sh` върви на тази машина на всеки 5 минути. Когато си idle и състоянието се е променило, ти праща съобщение, което започва с `TICK` и съдържа текущото състояние. То не е от потребителя.
- **При всяко `TICK` и в края на всеки свой ход** прави пълен цикъл:
  1. Сесии по машини: кой работи, кой е idle, кой е terminated.
  2. Всеки отворен PR към `feat/railway-hosting` със зелен CI и `mergeable`: провери го по „Ревю и мърдж“ по-долу и го мърджни веднага, ако е чист. ADR и `needs-human` само с одобрение от потребителя.
  3. Работник idle без PR над 30 мин: `ao send` с въпрос какво го блокира.
  4. Всеки таск, чиито зависимости вече са мърджнати и има свободно място на машина: пусни го.
  5. Обнови tracker #59 и label-ите.
- **Не приключвай ход**, докато има зелен, мърджабилен PR, който може да се мърджне без човек, или таск, който може да тръгне.

## Ревю и мърдж в `feat/railway-hosting`
Когато PR на работник е готов (зелен CI и работникът е idle; не разчитай на `--done` от отдалечен хост):
1. Issue → `status:in-review`. Пусни `ao review trigger` за неговия PR.
2. Провери: базата е `feat/railway-hosting`; CI е зелен (`gh pr checks`); няма AI trailers в commits (`git log --format=%B`); промените са в обхвата на таска; критериите за приемане в таск файла са изпълнени.
3. Ако има забележки: `ao send <session> "<конкретни забележки>"` и чакай нов `--done`.
4. Ако е чисто и **не е** ADR или `needs-human`: `gh pr merge <PR> --squash --match-head-commit <SHA> --repo petarnenov/ultra-factory`. `ao pr merge` не работи за PR-и на отдалечени работници (#63).
5. **Веднага след мърджа затвори таск issue-тата ръчно.** `Closes #N` в PR-а затваря issue само при мърдж в default бранча (`main`), а не във `feat/railway-hosting`. За всяко issue от `Closes #N` в тялото на PR-а: `gh issue close <N> --repo petarnenov/ultra-factory --reason completed --comment "Merged into feat/railway-hosting via #<PR> (<merge SHA>)."` и махни `status:*` label-ите. Ако с това се затварят всички таскове на стори, затвори и сторито. Ако се затварят всички сторита на епик, затвори и епика. Tracker-ът → Done.
6. ADR PR-и и всичко с `needs-human` се мърджват само след изрично „да“ от потребителя.
7. След всеки мърдж: ако `origin/main` е мръднал, синхронизирай `feat/railway-hosting` с `merge` като във фаза 1. Активните работници, на които предстои PR, правят `git merge origin/feat/railway-hosting` в своя бранч (никога rebase).
8. При конфликт между два PR-а: мърджни първия, а на втория работник прати `ao send` да мърджне `origin/feat/railway-hosting` в своя бранч и да реши конфликта.

## Наблюдение и комуникация
- Проверявай `ao session ls --host <H>` за трите машини при всяко `TICK` (виж „Будилник и цикъл на съгласуване“). Ако сесия е idle над 30 мин без PR, прати `ao send` за статус. Ако е мъртва, рестартирай таска (нова сесия, същият бранч, ако има push-нати commits).
- При `--stuck` или `--needs-input`: issue → `status:blocked` или `needs-human`, коментар в issue-то с причината, и кратко съобщение до потребителя.
- В края на всяка вълна: коментар в tracker issue-то с резюме (мърджнато, блокирано, следваща вълна) и същото резюме до потребителя.
- В края (всички таскове мърджнати): отвори **draft** PR `feat/railway-hosting → main` със заглавие `feat: Railway hosting (RW-1…RW-4)` и описание по епици. Не го мърджвай.

## Започни сега
Изпълни фаза 0, после фаза 1, после фаза 2. Покажи на потребителя:
- резултата от синхронизацията (SHA-ове и способности на машините),
- линк към tracker issue-то,
- плана на вълна 1 по машини.

След това пусни вълна 1.
