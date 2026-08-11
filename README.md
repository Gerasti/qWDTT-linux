# qWDTT CLI v0.9.0

CLI VPN клиент для Linux через TURN-серверы VK с WireGuard.

## Возможности

- Kernel WireGuard без sudo (capabilities)
- Управление профилями с приоритетами
- Auto-switch - переключение между профилями при сбоях
- SOCKS5 режим (без root, через wireproxy)
- Автоматическое переподключение после suspend/resume
- Read-only профили через NixOS конфигурацию (с поддержкой sops-nix)
- DNS resolvers: Yandex, Cloudflare, Google (UDP и DoH)
- D-Bus уведомления о подключениях и событиях
- Live вывод лога демона (`-log` флаг и `qwdtt log` команда)
- share <name> — показ ссылки и QR-кода для профиля
- Debug режим для мониторинга соединения

## Установка

### NixOS Module

Автоматически настраивает capabilities и kernel module

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
        deviceId = config.sops.secrets.pc0.path;
      };
    };

    enableBashIntegration = true;
    enableFishIntegration = true;
    # wrappers.enable = true;  # по умолчанию уже true при services.qwdtt.enable = true;
    # wrappers.group = "users";  # группа, которая может запускать wrapped бинарники
  };
}
```

Модуль автоматически:
- Установит `qwdtt`
- Создаст security wrapper `qwdtt` с `cap_net_admin` для работы без sudo (`services.qwdtt.wrappers.enable = true;` включён по умолчанию при `services.qwdtt.enable = true;`)
- Загрузит kernel module `wireguard`

Примените конфигурацию:
```bash
sudo nixos-rebuild switch
```

После установки `qwdtt` доступен через `/run/wrappers/bin/qwdtt`, `qwdtt`.

### Arch Linux

```bash
# Установить зависимости
sudo pacman -S iputils curl

# Скачать бинарник из Release или собрать через go build
# https://github.com/Gerasti/qWDTT-linux/releases
# Для сборки: sudo pacman -S go

# Сделать исполняемым
chmod +x qwdtt

# Опционально: переместить в /usr/local/bin для доступа без полного пути
# sudo mv qwdtt /usr/local/bin/

# Установить capabilities
sudo setcap cap_net_admin+eip qwdtt

# Опционально: установить автодополнение
# Bash:
sudo cp completions/qwdtt.bash /etc/bash_completion.d/qwdtt
# Fish:
mkdir -p ~/.config/fish/completions
cp completions/qwdtt.fish ~/.config/fish/completions/
```

### Debian/Ubuntu

```bash
# Установить зависимости
sudo apt update
sudo apt install iputils-ping curl libcap2-bin

# Скачать бинарник из Release или собрать через go build
# https://github.com/Gerasti/qWDTT-linux/releases
# Для сборки: sudo apt install golang-go

# Сделать исполняемым
chmod +x qwdtt

# Опционально: переместить в /usr/local/bin для доступа без полного пути
# sudo mv qwdtt /usr/local/bin/

# Установить capabilities
sudo setcap cap_net_admin+eip qwdtt

# Опционально: установить автодополнение
# Bash:
sudo cp completions/qwdtt.bash /etc/bash_completion.d/qwdtt
# Fish:
mkdir -p ~/.config/fish/completions
cp completions/qwdtt.fish ~/.config/fish/completions/
```

## Использование

```bash
# Добавить профиль
qwdtt add myserver "wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2"

# Импорт профилей из JSON (например, экспорт из мобильного клиента)
qwdtt import /path/to/profiles.json
qwdtt import profiles.json --dry-run      # просмотр без сохранения

# Подключиться
qwdtt con myserver

# Auto-switch режим
qwdtt con -auto-switch

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
qwdtt rm myserver mysrv2              # удалить несколько профилей
qwdtt show myserver mysrv2            # показать несколько профилей
qwdtt move myserver myserver-new      # переименовать профиль
qwdtt share myserver        # QR-код и share-ссылка
```

## Команды

```
qwdtt connect <profile> [флаги]      - Подключиться к VPN (alias: con)
qwdtt disconnect [profile]           - Отключиться от VPN (alias: discon)
qwdtt log [profile] [-n N] [-f]      - Показать лог демона (alias: lg)
qwdtt share <name>                   - Показать share-ссылку и QR-код
qwdtt debug                          - Показать debug информацию о соединении (alias: deb)
qwdtt add <name> <wdtt://...>        - Добавить профиль
qwdtt edit <name1> [name2] ... [флаги]  - Редактировать профили (флаги применяются ко всем)
qwdtt move <old_name> <new_name>      - Переименовать профиль (alias: mv)
qwdtt remove <name1> [name2] ...     - Удалить профили (alias: rm, запрашивает подтверждение, -y/-yes — без него)
qwdtt list [<group1> ...] [флаги]    - Список профилей, отфильтрованный по группам (alias: ls)
qwdtt show <name1> [name2] ...       - Показать профили (alias: sh)
qwdtt enable <name1> [name2] ...     - Включить профили (alias: en)
qwdtt disable <name1> [name2] ...    - Отключить профили (alias: dis)

# Управление группами: edit, remove, show, enable, disable, test поддерживают -group
qwdtt enable -group work             - Включить все профили группы "work"
qwdtt disable -group work            - Отключить все профили группы "work"
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
qwdtt import <file.json> [--dry-run] - Импортировать профили из JSON (--dry-run: просмотр без сохранения)
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
- `-socks-port PORT` - порт SOCKS5 (default: 9050, требуется с `-mode socks`)
- `-log` - выводить лог демона в терминал в реальном времени

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
- `-groups G1,G2` - установить группы профиля (через запятую, "" или "none" для очистки)

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
- Поддержка sops-nix для секретов (device_id, wdtt:// ссылки)
- Автоматически создаются для указанных пользователей

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

- Linux с WireGuard kernel module
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
├── suspend.go            # Мониторинг suspend/resume
├── url_parser.go         # Парсинг wdtt:// URL
├── wireguard_linux.go    # WireGuard интеграция
├── internal/core/        # Core библиотека (TURN, DTLS, DoH)
├── modules/nixos/        # NixOS module
├── completions/          # Bash/Fish автодополнение
├── flake.nix             # Nix flake конфигурация
└── go.mod                # Go dependencies
```

## Лицензия

GNU GPL-3.0
