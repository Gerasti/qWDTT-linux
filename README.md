# qWDTT CLI v0.5.0

CLI VPN клиент для Linux через TURN-серверы VK с WireGuard.

## Возможности

- Kernel WireGuard без sudo (capabilities)
- Управление профилями с приоритетами
- Auto-switch - переключение между профилями при сбоях
- Автоматическое переподключение после suspend/resume
- Read-only профили через NixOS конфигурацию (с поддержкой sops-nix)
- DNS resolvers: Yandex, Cloudflare, Google (UDP и DoH)
- Оптимизация CPU клиентского ядра
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
    # package = pkgs.qwdtt-cli;  # override package if needed
    deviceId = config.sops.secrets.wdtt-id.path; # Device ID for all profiles (path or string)

    users = [ "alice" ];

    profiles = {
    # read-only profiles can only be enabled/disabled
      work = {
        link = config.sops.secrets.work-server.path; # (path or string)
        priority = 100;
      };
      home = {
        link = config.sops.secrets.home-server.path;
      };
      backup1 = {
        link = config.sops.secrets.backup1.path;
      };
      backup2 = {
        link = config.sops.secrets.backup2.path;
      };
      mobile = {
        link = config.sops.secrets.mobile-server.path;
        deviceId = config.sops.secrets.wdtt-id-mobile.path;
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
- Установит `qwdtt-cli`, `wireguard-tools`, `iproute2`
- Создаст security wrappers с capabilities для работы без sudo
- Загрузит kernel module `wireguard`

Примените конфигурацию:
```bash
sudo nixos-rebuild switch
```

После установки `qwdtt-cli` доступен через `/run/wrappers/bin/qwdtt-cli`, `qwdtt-cli`.

### Arch Linux

```bash
# Установить зависимости
sudo pacman -S iproute2 wireguard-tools

# Скачать бинарник из Release или собрать через go build
# https://github.com/Gerasti/qWDTT-linux/releases
# Для сборки: sudo pacman -S go

# Сделать исполняемым
chmod +x qwdtt-cli

# Опционально: переместить в /usr/local/bin для доступа без полного пути
# sudo mv qwdtt-cli /usr/local/bin/

# Установить capabilities
sudo setcap cap_net_admin+eip qwdtt-cli

# Опционально: установить автодополнение
# Bash:
sudo cp completions/qwdtt-cli.bash /etc/bash_completion.d/qwdtt-cli
# Fish:
mkdir -p ~/.config/fish/completions
cp completions/qwdtt-cli.fish ~/.config/fish/completions/
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
chmod +x qwdtt-cli

# Опционально: переместить в /usr/local/bin для доступа без полного пути
# sudo mv qwdtt-cli /usr/local/bin/

# Установить capabilities
sudo setcap cap_net_admin+eip qwdtt-cli

# Опционально: установить автодополнение
# Bash:
sudo cp completions/qwdtt-cli.bash /etc/bash_completion.d/qwdtt-cli
# Fish:
mkdir -p ~/.config/fish/completions
cp completions/qwdtt-cli.fish ~/.config/fish/completions/
```

## Использование

```bash
# Добавить профиль
qwdtt-cli add myserver "wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2"

# Подключиться
qwdtt-cli con myserver

# Auto-switch режим
qwdtt-cli con -auto-switch

# С кастомным DNS resolver
qwdtt-cli con myserver -dns doh-cloudflare
qwdtt-cli con myserver -dns custom:8.8.8.8:53,1.1.1.1:53
qwdtt-cli con myserver -dns doh:https://dns.example.com/dns-query

# Debug информация о подключении
qwdtt-cli debug
# или watch -n 1 qwdtt-cli debug

# Отключиться
qwdtt-cli disconnect

# Управление
qwdtt-cli ls                    # список
qwdtt-cli edit myserver -priority 100
qwdtt-cli disable myserver
```

## Команды

```
qwdtt-cli connect <profile> [флаги]  - Подключиться к VPN
qwdtt-cli disconnect                 - Отключиться от VPN
qwdtt-cli debug                      - Показать debug информацию о соединении
qwdtt-cli add <name> <wdtt://...>    - Добавить профиль
qwdtt-cli edit <name> [флаги]        - Редактировать профиль
qwdtt-cli remove <name>              - Удалить профиль
qwdtt-cli list                       - Список профилей
qwdtt-cli show <name>                - Показать профиль
qwdtt-cli enable <name>              - Включить профиль
qwdtt-cli disable <name>             - Отключить профиль
qwdtt-cli device-id [id]             - Показать/установить Device ID
qwdtt-cli regenerate-id              - Перегенерировать Device ID
qwdtt-cli version                    - Версия
```

### Короткие алиасы

```
con    - connect
discon - disconnect
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
  - Опции: `auto`, `rjs`

## Флаги edit

- `-peer ADDR` - изменить адрес сервера (IP:PORT)
- `-password PASS` - изменить пароль
- `-hashes H1,H2` - изменить VK-хеши
- `-device-id ID` - изменить Device ID
- `-listen ADDR` - изменить локальный UDP адрес (default: 127.0.0.1:9000)
- `-priority N` - установить приоритет профиля (выше = раньше в auto-switch)

## Управление профилями

**Приоритеты:**
- Профили с более высоким приоритетом используются первыми в `-auto-switch`
- По умолчанию priority = 0
- Пример: `qwdtt-cli edit myserver -priority 100`

**Отключенные профили:**
- Не отображаются в интерактивном выборе
- Не используются в `-auto-switch`
- Можно подключиться явно: `qwdtt-cli con disabled-profile`

**Read-only профили (NixOS):**
- Управляются через NixOS конфигурацию
- Имена с префиксом `ro-` (например, `ro-work`)
- Нельзя редактировать или удалить через CLI
- Можно включать/отключать: `qwdtt-cli enable ro-work`
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

Пример: `qwdtt-cli con myserver -dns doh-cloudflare`

## Suspend/Resume

Автоматическое переподключение после пробуждения через systemd D-Bus. Работает без настройки на системах с systemd.

## Требования

- Linux с WireGuard kernel module
- `iproute2`, `wireguard-tools`
- `cap_net_admin` capabilities
- systemd (для suspend/resume)

## Структура проекта

```
.
├── cli.go                # Точка входа
├── connect.go            # Логика подключения
├── commands.go           # Команды управления профилями
├── profile.go            # Работа с профилями
├── config.go             # Конфигурация и Device ID
├── utils.go              # Вспомогательные функции
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
