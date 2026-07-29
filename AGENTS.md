# Freya: памятка для работы с репозиторием

Этот файл действует для всего репозитория. Подробная карта архитектуры и
текущий технический план находятся в `docs/PROJECT_MAP.md`.

## Коротко о проекте

- Это серверный эмулятор Cabal Online на Go (`go.mod`: Go 1.20).
- Исполняемые процессы: `masterserver`, `loginserver`, `gameserver`.
- `masterserver` владеет подключениями к MariaDB и координирует остальные
  серверы через двунаправленный RPC поверх TCP/gob.
- `loginserver` принимает клиент на TCP 38101, проверяет версию/учётную запись
  и отдаёт список игровых каналов.
- `gameserver` принимает клиент на TCP 38111, ведёт игровые миры и обрабатывает
  игровые пакеты. Изменения постоянного состояния отправляет в `masterserver`
  по RPC.
- Общие протоколы и модели находятся в `share/`.
- Карты, мобы, варпы и начальные данные загружаются из `data/`; расширения
  игрового поведения на Lua находятся в `script/`.

README говорит об EP8, имя рабочей папки — EP6, а текущий
`cfg/loginserver.ini` использует `client_version=67`. Не угадывать версию
протокола по одному из этих признаков: перед изменением пакета сверять код
операции, размер и порядок полей с целевым клиентом пользователя.

## Перед любым изменением

1. Проверить рабочее дерево и не перезаписывать изменения пользователя:
   `git -c safe.directory='C:/Cabal/Freya EP6' status --short --branch`.
2. Прочитать релевантные точки регистрации пакетов/RPC до изменения
   обработчика.
3. Снять исходный compile baseline командой `go test ./...` с доступным
   временным `GOCACHE`.
4. Не делать массовый `gofmt` всего репозитория: в старом коде много
   неформатированных файлов. Форматировать только изменённые `.go`-файлы.
5. Не редактировать локальные `cfg/*.ini`, SQL или игровые данные без прямой
   необходимости задачи. В рабочем дереве могут быть незавершённые изменения.

Из-за Windows ownership обычные команды Git в этой рабочей копии могут
получать `dubious ownership`. Использовать одноразовый параметр
`-c safe.directory='C:/Cabal/Freya EP6'`; не менять глобальный Git config.

## Куда идти за кодом

| Задача | Основные места |
|---|---|
| Запуск и порядок инициализации | `cmd/*server/main.go`, `cmd/*server/def/` |
| Login-пакет | `cmd/loginserver/packet/const.go`, `packet.go`, нужный handler |
| Game-пакет | `cmd/gameserver/packet/const.go`, `packet.go`, нужный handler |
| RPC-контракт | `share/rpc/const.go`, `share/models/**` |
| RPC-реализация и SQL | `cmd/masterserver/rpc/` |
| Обратный RPC в Login/Game | `cmd/loginserver/rpc/`, `cmd/gameserver/rpc/` |
| TCP-сессия и бинарный пакет | `share/network/` |
| RPC transport | `share/rpc/` |
| Персонаж, инвентарь, навыки | `share/models/` |
| Мир, клетки, мобы, pathfinding | `cmd/gameserver/game/`, `context/` |
| Конфигурация | `cmd/*server/def/config.go`, `share/conf/`, `cfg/` |
| SQL-схемы | `sql/login.sql`, `sql/world.sql` |
| YAML-данные | `data/` |
| Lua API и скрипты | `share/script/`, `cmd/*server/packet/script.go`, `script/` |

## Контракты, которые меняются комплектом

### Клиентский пакет

При добавлении или изменении пакета проверить весь маршрут:

1. opcode в `cmd/<server>/packet/const.go`;
2. регистрацию в `cmd/<server>/packet/packet.go`;
3. handler с точными типами и порядком полей;
4. ответ/notification через `network.Writer`;
5. общую модель, RPC и SQL, если меняется постоянное состояние.

`network.Reader` и `network.Writer` работают с little-endian числами и не
делают предметной валидации. Нельзя без проверки менять ширину поля
(`byte`/`uint16`/`uint32`), padding или последовательность записи.

### RPC

Для нового вызова обычно нужны:

1. строковая константа в `share/rpc/const.go`;
2. request/response DTO в `share/models/<domain>/`;
3. handler в `cmd/masterserver/rpc/`;
4. регистрация в `cmd/masterserver/rpc/rpc.go`;
5. регистрация в `cmd/loginserver/rpc/rpc.go` или
   `cmd/gameserver/rpc/rpc.go`, если Master вызывает сервер обратно.

RPC использует gob и отражение. Обе стороны должны собираться с совместимыми
экспортируемыми DTO, а handler обязан иметь сигнатуру
`func(*rpc.Client, request, *response) error`.

### Постоянное игровое состояние

- Login DB: `accounts`, `sub_password`.
- World DB: `characters`, `characters_equipment`,
  `characters_inventory`, `characters_quickslots`, `characters_skills`,
  `lobby_metadata`.
- Номер игрового сервера выбирает World DB через
  `DatabaseManager.Get(serverID)`.
- `Inventory`, `Equipment` и `Links` после `Setup(...)` синхронизируют
  изменения с Master по RPC. Не обновлять только локальную map, если состояние
  должно пережить переподключение.
- Составные операции инвентаря/экипировки должны быть атомарными на уровне БД
  либо иметь проверенный rollback.

### Игровой runtime

- Специфичный для игрока runtime лежит в `network.Session.DataEx` как
  `*context.Context`.
- До выбора персонажа использовать `context.PreParse`; после выбора —
  `context.Parse`.
- Мир делится на клетки; рассылка игрокам, мобам и предметам проходит через
  `World`/`Cell`.
- При работе с `Session`, `Context`, `World`, `Cell`, моделями инвентаря и
  глобальной event/RPC системой учитывать конкурентные goroutine и текущие
  mutex.

## Проверка

В PowerShell:

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'freya-go-build'
go test ./...
```

Тестовых файлов сейчас нет, поэтому эта команда в первую очередь является
проверкой компиляции всех пакетов.

Для изменённых файлов:

```powershell
gofmt -w <изменённые-go-файлы>
go test ./...
go vet ./...
```

Сборка без зависимости от Unix-команд в Makefile:

```powershell
go build -buildvcs=false -o bin/masterserver.exe ./cmd/masterserver
go build -buildvcs=false -o bin/loginserver.exe ./cmd/loginserver
go build -buildvcs=false -o bin/gameserver.exe ./cmd/gameserver
```

`-buildvcs=false` нужен в этой рабочей копии из-за Git ownership; на код и
runtime-поведение он не влияет.

Запускать из структуры, где бинарники лежат в `bin/`: `directory.Root()`
вычисляет корень проекта из пути исполняемого файла и ищет рядом `cfg/`,
`data/` и `script/`.

Обычный порядок локального запуска:

1. импортировать `sql/login.sql` и `sql/world.sql` в MariaDB;
2. настроить `cfg/masterserver.ini`, `cfg/loginserver.ini`,
   `cfg/gameserver_<serverID>_<channelID>.ini`;
3. запустить `masterserver`;
4. запустить `loginserver`;
5. запустить `gameserver [serverID channelID]`, например `gameserver 1 1`.

## Локальный lifecycle серверов

GoLand не управляет локальным runtime. По запросу пользователя Codex сам
собирает, запускает, останавливает и перезапускает все три процесса через
PowerShell.

- Перед запуском проверить владельцев портов 9001, 38101 и 38111 и точные пути
  уже работающих процессов.
- Собирать канонические `bin/masterserver.exe`, `bin/loginserver.exe` и
  `bin/gameserver.exe` с `-buildvcs=false`.
- Запускать скрытыми фоновыми процессами с working directory в корне проекта.
- Порядок запуска: Master -> дождаться DB и 9001 -> Login -> дождаться
  регистрации -> Game с аргументами `1 1`.
- Порядок остановки обратный: Game -> Login -> Master. Останавливать по
  проверенному PID/пути, не массово по приблизительному имени.
- Runtime stdout/stderr направлять в `log/codex-runtime/`; основные application
  logs остаются в `log/masterserver.log`, `log/loginserver.log` и
  `log/gameserver_1_1.log`.
- Готовность подтверждать одновременно живым процессом, LISTENING-портом и
  регистрацией Login/Game в логе Master.

## Критерий готовности изменения

- Форматированы только затронутые Go-файлы.
- `go test ./...` компилирует все пакеты либо в отчёте явно отделена
  существовавшая до задачи ошибка.
- Для изменения протокола проверены opcode, точный бинарный layout и обе
  стороны RPC.
- Для изменения данных проверены SQL-схема, загрузка и сохранение после
  повторного входа.
- Для сетевой/конкурентной логики по возможности выполнен `go test -race`.
- При изменении архитектуры обновлён `docs/PROJECT_MAP.md`.
