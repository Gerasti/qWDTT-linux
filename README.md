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
qwdtt-cli = builtins.getFlake "/etc/qWDTT-linux"; # local path after git clone
# either with internet https://github.com/Gerasti/qWDTT-linux
in
{
  imports =
  [
    qwdtt-cli.nixosModules.qwdtt-cli
  ];
  services.qwdtt-cli = {
    enable = true;
    # package = pkgs.qwdtt;  # override package if needed
    deviceId = config.sops.secrets.wdtt-id.path; # Device ID for all profiles (path or string)

    users = [ "alice" ];

    profiles = {
    # read-only profiles can only be enabled/disabled
      work = {
        # alice has access to sops file
        link = config.sops.secrets.work-server.path; # (path or string)
        priority = 100;
      };
      home = {
        link = config.sops.secrets.home-server.path;
      };
      guest = {
        link = config.sops.secrets.guest-server.path;
        deviceId = config.sops.secrets.wdtt-id-guest.path;
      };
    };

    enableBashIntegration = true;
    enableFishIntegration = true;
    wrappers = {
      enable = true;  # create security wrappers with capabilities (allows running without sudo)
      # group = "users";  # group that can execute wrapped binaries
    };
  };
}
```

Модуль автоматически:
- Установит `qwdtt`, `wireguard-tools`, `iproute2`
- Создаст security wrappers с capabilities для работы без sudo
- Загрузит kernel module `wireguard`

Примените конфигурацию:
```bash
sudo nixos-rebuild switch
```

После установки `qwdtt` доступен через `/run/wrappers/bin/qwdtt`, `qwdtt`.

### Arch Linux

```bash
# Установить зависимости
sudo pacman -S iproute2 wireguard-tools

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
sudo apt install iproute2 wireguard-tools libcap2-bin

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
qwdtt edit myserver -priority 100
qwdtt disable myserver
qwdtt share myserver        # QR-код и share-ссылка
```

## Команды

```
qwdtt connect <profile> [флаги]      - Подключиться к VPN (alias: con)
qwdtt disconnect [profile]           - Отключиться от VPN (alias: discon)
qwdtt log [profile] [-n N] [-f]      - Показать лог демона (alias: lg)
qwdtt share <name>                   - Показать share-ссылку и QR-код
qwdtt debug                          - Показать debug информацию о соединении
qwdtt add <name> <wdtt://...>        - Добавить профиль
qwdtt edit <name> [флаги]            - Редактировать профиль
qwdtt remove <name>                  - Удалить профиль (alias: rm)
qwdtt list                           - Список профилей (alias: ls)
qwdtt show <name>                    - Показать профиль (alias: sh)
qwdtt enable <name>                  - Включить профиль (alias: en)
qwdtt disable <name>                 - Отключить профиль (alias: dis)
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
```

## Флаги connect

- `-auto-switch` - переключение между профилями при сбоях
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

## Флаги edit

- `-peer ADDR` - изменить адрес сервера (IP:PORT)
- `-password PASS` - изменить пароль
- `-hashes H1,H2` - изменить VK-хеши
- `-device-id ID` - изменить Device ID
- `-listen ADDR` - изменить локальный UDP адрес (default: 127.0.0.1:9000)
- `-priority N` - установить приоритет профиля (выше = раньше в auto-switch)

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
- `iproute2`, `wireguard-tools`
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
