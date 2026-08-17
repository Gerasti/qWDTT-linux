# qWDTT linux v0.9.5

CLI VPN клиент для Linux через TURN-серверы VK с WireGuard или Raw.

## Возможности

- TUN/TAP без sudo (capabilities)
- Управление профилями с приоритетами
- Auto-switch - переключение между профилями при сбоях
- SOCKS5 режим (без root, через gVisor; несколько socks с разными портами)
- Raw TUN/TAP режим (сырые IP-пакеты, минуя WireGuard)
- Автоматическое переподключение после suspend/resume
- Read-only профили через NixOS конфигурацию (с поддержкой sops-nix)
- DNS resolvers: Yandex, Cloudflare, Google (UDP и DoH)
- D-Bus уведомления о подключениях и событиях
- Live вывод лога демона (`-log` флаг и `qwdtt log` команда)
- share <name> — показ ссылки и QR-кода для профиля
- import <name> — импорт профилей из JSON или ZIP файлов приложения андроид
- Debug режим для мониторинга соединения

## Установка

### NixOS Module

Автоматически настраивает capabilities

**Пример конфигурации (`/etc/nixos/qwdtt-cli.nix`):**

```nix
{ config, lib, pkgs, ... }:
let
qwdtt = builtins.getFlake "/etc/qWDTT-linux"; # local path after git clone
# either with internet https://github.com/Gerasti/qWDTT-linux
in
{
  imports =
  [
    qwdtt.nixosModules.qwdtt
  ];
  services.qwdtt = {
    enable = true;
    # package = pkgs.qwdtt;  # override package if needed
    deviceId = config.sops.secrets.wdtt-id.path; # Device ID for all profiles (path or string)

    users = [ "alice" ];

    profiles = {
    # read-only profiles can only be enabled/disabled
      phone0 = {
        # alice has access to sops file
        link = config.sops.secrets.phone0.path; # (path or string)
        priority = 100;
      };
      phone1 = {
        link = config.sops.secrets.phone1.path;
      };
      pc0 = {
        link = config.sops.secrets.pc0.path;
        deviceId = config.sops.secrets.pc0.path; # particular Id for profile
      };
    };

    enableBashIntegration = true;
    enableFishIntegration = true;
    # wrappers.enable = true;  # по умолчанию уже true
    # wrappers.group = "users";  # группа, которая может запускать wrapped бинарники
  };
}
```

Модуль автоматически:
- Установит `qwdtt`
- Создаст security wrapper `qwdtt` с `cap_net_admin` для работы без sudo (`services.qwdtt.wrappers.enable = true;`)

Примените конфигурацию:
```bash
sudo nixos-rebuild switch
```

После установки `qwdtt` доступен через `/run/wrappers/bin/qwdtt`, `qwdtt`.

## Зависимости установки

### Debian/Ubuntu

```bash
sudo apt install iputils-ping curl patchelf
```

### Arch Linux

```bash
sudo pacman -S iputils curl patchelf
```

### Fedora

```bash
sudo dnf install iputils curl patchelf
```

### Скачивание и установка бинарника из Release

```bash
# Скачать бинарник из Release
curl -L -o qwdtt https://github.com/Gerasti/qWDTT-linux/releases/download/v0.9.5/qwdtt

# Указать правильный интерпретатор (glibc) и сделать исполняемым
patchelf --set-interpreter /lib64/ld-linux-x86-64.so.2 qwdtt
chmod +x qwdtt

# Опционально: переместить в /usr/local/bin для доступа без полного пути
# sudo mv qwdtt /usr/local/bin/

# Установить capabilities
sudo setcap cap_net_admin+eip qwdtt
```

### Установка автодополнения

```bash
# Bash:
sudo cp completions/qwdtt.bash /etc/bash_completion.d/qwdtt
# Fish:
mkdir -p ~/.config/fish/completions
cp completions/qwdtt.fish ~/.config/fish/completions/
```

### Сборка из исходников

Минимальная версия Go — **1.24**.

```bash
# Установить Go (если ещё не установлен)
wget https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Собрать из исходников
git clone https://github.com/Gerasti/qWDTT-linux
cd qWDTT-linux
go build -trimpath -ldflags="-s -w" 

# Опционально: переместить в /usr/local/bin для доступа без полного пути
# sudo mv qwdtt /usr/local/bin/

# Установить capabilities
sudo setcap cap_net_admin+eip qwdtt
```

## Использование

```bash
# Добавить профиль (wdtt:// ссылка)
qwdtt add myserver "wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2"

# Добавить профиль (qwdtt://config ссылка — более гибкий формат с query-параметрами)
qwdtt add myserver "qwdtt://config?name=МойСервер&peer=1.2.3.4:56000&hashes=hash1,hash2&workers=18&port=9000&pass=secret"

# Добавить профиль по qwdtt://config ссылке (имя берётся из параметра name)
qwdtt add "qwdtt://config?name=МойСервер&peer=1.2.3.4:56000&hashes=hash1,hash2&workers=18&port=9000&pass=secret"

# Импорт профилей из JSON или ZIP (например, экспорт из мобильного клиента)
qwdtt import /path/to/profiles.json
qwdtt import /path/to/profiles.zip
qwdtt import profiles.json --dry-run      # просмотр без сохранения

# Подключиться
qwdtt con myserver

# Auto-switch режим с режимом Raw
qwdtt con -auto-switch --mode raw

# С кастомным DNS resolver
qwdtt con myserver -dns doh-cloudflare
qwdtt con myserver -dns custom:8.8.8.8:53,1.1.1.1:53
qwdtt con myserver -dns doh:https://dns.example.com/dns-query

# Debug информация о подключении
qwdtt debug
# или watch -n 1 qwdtt debug

# Просмотр лога демона (последние 20 строк)
qwdtt log autoswitch -n 20

# Просмотр лога в реальном времени
qwdtt log autoswitch -f

# Live лог при подключении
qwdtt con -auto-switch -log

# Отключить текущий профиль autoswitch (переключится на следующий)
qwdtt discon <current-profile-name>

# Отключиться
qwdtt disconnect

# Управление
qwdtt ls                    # список
qwdtt ls work               # список профилей в группе "work"
qwdtt ls work personal      # список профилей в группах "work" или "personal"
qwdtt edit myserver -priority 100
qwdtt edit srv1 srv2 -priority 100   # изменить приоритет нескольких профилей
qwdtt edit myserver -groups work,personal
qwdtt edit myserver -groups ""
qwdtt disable myserver mysrv2         # отключить несколько профилей
qwdtt enable myserver mysrv2          # включить несколько профилей
qwdtt enable -ro                      # включить все read-only профили
qwdtt disable -ro                     # отключить все read-only профили
qwdtt rm myserver mysrv2              # удалить несколько профилей
qwdtt show myserver mysrv2            # показать несколько профилей
qwdtt move myserver myserver-new      # переименовать профиль
qwdtt share myserver        # QR-код и share-ссылка (wdtt:// формат)
qwdtt share myserver -qwdtt  # QR-код и qwdtt://config? ссылка
```

## Команды

```
qwdtt connect <profile> [флаги]      - Подключиться к VPN (alias: con)
qwdtt disconnect [profile]           - Отключиться от VPN (alias: discon)
qwdtt log [profile] [-n N] [-f]      - Показать лог демона (alias: lg)
qwdtt share <name> [-qwdtt]          - Показать share-ссылку и QR-код (-qwdtt: qwdtt://config? формат)
qwdtt debug                          - Показать debug информацию о соединении (alias: deb)
qwdtt add <name> <wdtt://...|qwdtt://config?name=...> - Добавить профиль
qwdtt edit <name1> [name2] ... [флаги]  - Редактировать профили (флаги применяются ко всем)
qwdtt move <old_name> <new_name>      - Переименовать профиль (alias: mv)
qwdtt remove <name1> [name2] ...     - Удалить профили (alias: rm, запрашивает подтверждение, -y/-yes — без него)
qwdtt list [<group1> ...] [флаги]    - Список профилей, отфильтрованный по группам (alias: ls)
qwdtt show <name1> [name2] ...       - Показать профили (alias: sh)
qwdtt enable <name1> [name2] ...     - Включить профили (alias: en, -ro чтобы только read-only)
qwdtt disable <name1> [name2] ...    - Отключить профили (alias: dis, -ro чтобы только read-only)

# Управление группами: edit, remove, show, enable, disable, test поддерживают -group
qwdtt enable -group work             - Включить все профили группы "work"
qwdtt disable -group work            - Отключить все профили группы "work"
qwdtt en -ro                         - Включить все read-only профили (alias: enable -ro)
qwdtt show -group work               - Показать все профили группы "work"
qwdtt remove -group work             - Удалить все профили группы "work"
qwdtt edit -group work -priority 100 - Изменить все профили группы "work"
qwdtt test -group work               - Протестировать все профили группы "work"

# Маски (glob): edit, remove, show, enable, disable, test принимают маски * ? [abc]
qwdtt rm 'wdtt_*'                    - Удалить все профили, начинающиеся с wdtt_ (с подтверждением)
qwdtt rm 'wdtt_*' -y                 - То же без подтверждения
qwdtt test 'wdtt_*'                  - Протестировать все профили по маске
# ВАЖНО: в fish и bash маску нужно заключать в кавычки, иначе её раскроет сам шелл
# (в fish ошибка "No matches for wildcard" — это ошибка шелла, решается кавычками)
qwdtt import <file.json|file.zip> [--dry-run] - Импортировать профили из JSON или ZIP (--dry-run: просмотр без сохранения)
qwdtt subscription <add|remove|show|update> [флаги] - Управление подписками (alias: sub)
  add <url>                    - Добавить подписку (имя берётся из JSON subscriptionName)
  add <name> <url>             - Добавить подписку с явным именем
  remove <name> [-y]           - Удалить подписку и все её профили (alias: rm)
  show [<name>]                - Показать подписку или список всех (alias: sh)
  update [<name>] [-y]         - Обновить профили из подписки (перезаписывает все профили)
qwdtt device-id [id]                 - Показать/установить Device ID (alias: id)
qwdtt regenerate-id                  - Перегенерировать Device ID
qwdtt version                        - Версия
```

### Короткие алиасы

```
con    - connect
discon - disconnect
lg     - log
sh     - show
ls     - list
rm     - remove
id     - device-id
en     - enable
dis    - disable
deb    - debug
sub    - subscription
```

## Флаги connect

- `-auto-switch` - переключение между профилями при сбоях
- `-toggle` - остановить запущенный профиль (или autoswitch), либо запустить если не запущен
- `-workers N` - количество воркеров (кратно 9, default: 9)
- `-mtu N` - MTU туннеля (default: 1280, max: 1500)
- `-timeout N` - таймаут для auto-switch (default: 120)
- `-hashes H1,H2` - переопределить VK-хеши профиля
- `-dns RESOLVER` - DNS resolver (default: yandex)
  - Опции: `yandex`, `cloudflare`, `google`
  - DoH: `doh-yandex`, `doh-cloudflare`, `doh-google`
  - Кастомный UDP: `custom:8.8.8.8:53,1.1.1.1:53`
  - Кастомный DoH: `doh:https://dns.example.com/dns-query`
- `-captcha MODE` - режим обхода captcha (default: auto)
  - Опции: `auto`, `rjs`, `wv`
- `-mode MODE` - режим подключения (default: tun)
  - Опции: `tun` — прямой WireGuard через kernel; `socks` — локальный SOCKS5 прокси
  - `raw` — сырой IP-режим через TUN/TAP интерфейс, лучшая оптимизация на сервере 
- `-socks-port PORT` - порт SOCKS5 (default: 9050, требуется с `-mode socks`)
- `-raw-port PORT` - порт для raw TUN режима (default: 56003, требуется с `-mode raw`)
- `-transport TRANSPORT` - транспорт до TURN-relay: `udp` или `tcp` (default: udp). Использовать `tcp`, если UDP до TURN-relay блокируется
- `-log` - выводить лог демона в терминал в реальном времени
- `-bl` / `--black-list` - обход туннеля для указанных доменов/IP/CIDR: они идут напрямую, остальное — через туннель (режимы `tun`, `raw` и `socks`)
  - Через запятую, например: `-bl vk.ru,yandex.ru`
  - В режиме `socks` bypass работает на уровне SOCKS5-прокси: запросы к этим доменам/IP идут напрямую, не через туннель
- `-bl-file PATH` / `--black-list-file` - прочитать домены из JSON-файла (поле `bypassRoutes`), можно комбинировать с `-bl`
  - Пример: `-bl-file ./qwdtt_bypass_sites.json`

## Флаги list

- `-en` / `-enabled` - показать только включённые профили
- `-dis` / `-disabled` - показать только отключённые профили
- `-ro` - показать только read-only профили
- `<group1> [group2] ...` - позиционный аргумент: показать только профили из указанных групп (поддерживается несколько)

  Примеры: `qwdtt ls work`, `qwdtt ls work personal`, `qwdtt ls -ro`, `qwdtt ls -en work`

## Флаги edit

- `-peer ADDR` - изменить адрес сервера (IP:PORT)
- `-password PASS` - изменить пароль
- `-hashes H1,H2` - изменить VK-хеши
- `-device-id ID` - изменить Device ID
- `-listen ADDR` - изменить локальный UDP адрес (default: 127.0.0.1:9000)
 - `-priority N` - установить приоритет профиля (выше = раньше в auto-switch)
 - `-workers N` - установить количество воркеров (должно быть кратно 9: 9, 18, 27, ...)
 - `-groups G1,G2` - установить группы профиля (через запятую, "" или "none" для очистки)

## Флаги enable/disable

- `-group GROUP` - включить/отключить все профили в группе
- `-ro` - только read-only профили (имена с префиксом `ro-`)

## Флаги test

- `-ro` - тестировать только read-only профили
- `-en` / `-enabled` - тестировать только включённые профили
- `-dis` / `-disabled` - тестировать только отключённые профили
- `-group GROUP` - тестировать все профили группы
- `-mode MODE` - режим подключения: `tun` или `socks` (default: tun)
- `-socks-port PORT` - порт SOCKS5 (default: 9050, с `-mode socks`)
- `-timeout N` - таймаут подключения в секундах (default: 10)
- `-delay N` - пауза между профилями в секундах (default: 5)

  Примеры: `qwdtt test --delay 2`, `qwdtt test myserver mysrv2 --delay 10`

## Auto-switch и режимы подключения

**Auto-switch (autoswitch):**
- Только один autoswitch daemon может быть запущен одновременно
- Autoswitch работает в режиме `tun` (по умолчанию) или `socks` (`--mode socks`)
- При подключении можно переключать профили: `qwdtt discon <current-profile>`
- В режиме autoswitch все профили используют один и тот же режим (tun или socks)

**Режим tun:**
- Только одно активное tun-соединение (через WireGuard kernel interface `wg-qwdtt`)

**Режим socks:**
- SOCKS5 прокси через gVisor (без root)
- Несколько SOCKS5 соединений возможны одновременно с разными портами (`-socks-port PORT`)

**Режим raw:**
- Использует `netlink` для настройки маршрутов и IP-адреса
- Флаг `-raw-port PORT` задаёт порт для обратного соединения (default: 56003)

## Форматы ссылок

Поддерживаются два формата ссылок для добавления профилей:

**1. wdtt:// (позиционный формат):**
```
wdtt://IP:DTLSPort:PORT2:PORT3:password:hash1,hash2#Name
```
Пример: `wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2#MyServer`

**2. qwdtt://config (query-параметры, более гибкий):**
```
qwdtt://config?name=Имя&peer=IP:DTLSPort&hashes=hash1,hash2&workers=18&port=9000&pass=пароль
```
Параметры:
- `name` — имя профиля (обязательно для `qwdtt add <URL>`)
- `peer` — адрес сервера в формате `IP:PORT` (обязательно)
- `hashes` — VK хеши через запятую (опционально)
- `workers` — количество воркеров (по умолчанию 9)
- `port` — локальный порт прослушивания (по умолчанию 9000)
- `pass` — пароль (обязательно)

Оба формата поддерживаются в командах `add`, `test` и `share -qwdtt`.

## Управление профилями

**Приоритеты:**
- Профили с более высоким приоритетом используются первыми в `-auto-switch`
- По умолчанию priority = 0
- Пример: `qwdtt edit myserver -priority 100`

**Отключенные профили:**
- Не отображаются в интерактивном выборе
- Не используются в `-auto-switch`
- Можно подключиться явно: `qwdtt con disabled-profile`

**Read-only профили (NixOS):**
- Управляются через NixOS конфигурацию
- Имена с префиксом `ro-` (например, `ro-work`)
- Нельзя редактировать или удалить через CLI
- Можно включать/отключать: `qwdtt enable ro-work`
- С флагом `-ro` можно включать/отключать все read-only профили сразу: `qwdtt enable -ro`, `qwdtt disable -ro`
  - Поддержка sops-nix для секретов (device_id, wdtt:// ссылки)
  - Автоматически создаются для указанных пользователей

## Подписки (Subscriptions)

Подписки позволяют получать и автоматически обновлять наборы профилей с HTTPS-сервера.

### Формат JSON подписки

```json
{
  "subscriptionName": "DarkBit VPN",
  "description": "Подписка · до 24.08.2026",
  "profiles": [
    {
      "name": "Германия",
      "peer": "144.31.223.80:56000",
      "password": "wiGm3McD5R",
      "hashes": "vk_hash_1,vk_hash_2",
      "workersPerHash": 16,
      "listenPort": 9000
    }
  ]
}
```

Поддерживаются алиасы: `groupName` вместо `subscriptionName`, `servers` вместо `profiles`,
`vkHashes` вместо `hashes`, `workersPerHash` вместо `workers`, `listenPort` вместо `port`,
`pass` вместо `password`. Ответ может быть JSON или Base64 с JSON внутри.

### Команды

| Команда | Описание |
|---------|----------|
| `qwdtt sub add <url>` | Добавить подписку (имя из `subscriptionName` в JSON) |
| `qwdtt sub add <name> <url>` | Добавить подписку с явным именем |
| `qwdtt sub show [<name>]` | Показать подписку или список всех |
| `qwdtt sub update [<name>]` | Обновить профили из подписки (заменяет все профили) |
| `qwdtt sub upd [<name>]` | alias для update |
| `qwdtt sub rm <name>` | Удалить подписку и все её профили |

Флаги: `-y` / `-yes` — пропустить подтверждение.

### Ограничения подписок

- Профили из подписки создаются как обычные профили, но находятся в группе с именем подписки
- Группу подписки **нельзя** удалить из профиля (`edit -groups` заблокирован)
- В группу подписки **нельзя** добавить другие профили
- Профили подписки нельзя удалить через `qwdtt rm` — используйте `qwdtt sub rm`
- `qwdtt sub update` **заменяет** все профили подписки (удаляет старые, создаёт новые)
- НестSubscription-флаги (`edit -priority`, `-peer`, `-workers` и т.д.) работают как обычно

### Примеры

```bash
# Добавить подписку (имя из JSON)
qwdtt sub add https://example.com/sub.json -y

# Добавить подписку с явным именем
qwdtt sub add "My VPN" "https://example.com/sub.json"

# Показать все подписки
qwdtt sub show

# Обновить конкретную подписку
qwdtt sub update "DarkBit VPN"

# Обновить все подписки
qwdtt sub update

# Удалить подписку и все её профили
qwdtt sub rm "DarkBit VPN" -y
```

## DNS Resolvers

Поддерживаются следующие DNS resolvers:

**Стандартные UDP:**
- `yandex` (default) - 77.88.8.8, 77.88.8.1
- `cloudflare` - 1.1.1.1, 1.0.0.1
- `google` - 8.8.8.8, 8.8.4.4
- `custom:IP:PORT,IP:PORT` - кастомные UDP серверы

**DNS-over-HTTPS (DoH):**
- `doh-yandex` - https://common.dot.dns.yandex.net/dns-query
- `doh-cloudflare` - https://cloudflare-dns.com/dns-query
- `doh-google` - https://dns.google/dns-query
- `doh:https://...` - кастомный DoH endpoint

Пример: `qwdtt con myserver -dns doh-cloudflare`

## Suspend/Resume

Автоматическое переподключение после пробуждения через systemd D-Bus. Работает без настройки на системах с systemd.

## Требования

- Linux
- `iputils` (ping), `curl`
- `cap_net_admin` capabilities
- systemd (для suspend/resume)
- D-Bus session bus (для уведомлений)

## Структура проекта

```
.
├── cli.go                # Точка входа
├── connect.go            # Логика подключения
├── commands.go           # Команды управления профилями
├── daemon.go             # Демонизация и PID-файлы
├── config.go             # Конфигурация и Device ID
├── utils.go              # Вспомогательные функции
├── notify.go             # D-Bus уведомления
├── captcha_socket.go     # Сокет для капчи
├── profile.go            # Работа с профилями
├── subscription.go       # Управление подписками (HTTPS JSON)
├── suspend.go            # Мониторинг suspend/resume
├── test.go               # Команда test (VKAuth, Workers, Connect, InternetCheck)
├── url_parser.go         # Парсинг wdtt:// URL
├── wireguard_linux.go    # WireGuard интеграция (netlink/wgctrl, capabilities)
├── internal/core/        # Core библиотека (TURN, DTLS, DoH)
├── modules/nixos/        # NixOS module
├── completions/          # Bash/Fish автодополнение
├── flake.nix             # Nix flake конфигурация
└── go.mod                # Go dependencies
```

## Лицензия

GNU GPL-3.0
