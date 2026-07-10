# qWDTT CLI

CLI версия VPN клиента Linux через TURN-серверы VK с поддержкой WireGuard. Легко подключается в скрипты и мало весит.

## Возможности

- Подключение к VPN через TURN-серверы VK
- Управление профилями (добавление, редактирование, удаление)
- Kernel WireGuard без sudo (через capabilities)
- Split-routing (0.0.0.0/1 + 128.0.0.0/1)
- Автоматическое исключение TURN-серверов из VPN маршрутов

## Установка

### NixOS Module

Автоматически настраивает capabilities и kernel module

**Пример конфигурации (`/etc/nixos/qwdtt-cli.nix`):**

```nix
{ config, lib, pkgs, ... }:
let
qwdtt-cli = builtins.getFlake "/etc/qWDTT-linux"; 
# either with internet https://github.com/Gerasti/qWDTT-linux
in
{
  imports =
  [
    qwdtt-cli.nixosModules.qwdtt-cli
  ];
  services.qwdtt-cli = {
    enable = true;
    useVendor = true;  # if false, Go modules will be downloaded from network during build
    # package = pkgs.qwdtt-cli;  # override package if needed
    deviceId = config.sops.secrets.wdtt-id.path; # Device ID for all profiles (path or string)
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
```

## Использование

```bash
# Добавить профиль
qwdtt-cli add myserver "wdtt://1.2.3.4:56000:56001:0:password:hash1,hash2#MyServer"

# Подключиться
qwdtt-cli con myserver

# Список профилей
qwdtt-cli ls

# Показать профиль
qwdtt-cli sh myserver

# Редактировать профиль
qwdtt-cli edit myserver -password newpass

# Удалить профиль
qwdtt-cli rm myserver
```

## Команды

```
qwdtt-cli connect <profile> [флаги]  - Подключиться к VPN
qwdtt-cli add <name> <wdtt://...>    - Добавить профиль
qwdtt-cli edit <name> [флаги]        - Редактировать профиль
qwdtt-cli remove <name>              - Удалить профиль
qwdtt-cli list                       - Список профилей
qwdtt-cli show <name>                - Показать профиль
qwdtt-cli device-id [id]             - Показать/установить Device ID
qwdtt-cli regenerate-id              - Перегенерировать Device ID
qwdtt-cli version                    - Версия
```

### Короткие команды

```
con  - connect
sh   - show
ls   - list
rm   - remove
id   - device-id
```

### Флаги connect

- `-workers N` - Количество воркеров, кратно 9 (default: 9)
- `-mtu N` - MTU туннеля (default: 1280, max: 1500)
- `-hashes H1,H2` - Переопределить VK-хеши профиля

### Флаги edit

- `-peer ADDR` - Изменить адрес сервера (IP:PORT)
- `-password PASS` - Изменить пароль
- `-hashes H1,H2` - Изменить VK-хеши
- `-device-id ID` - Изменить Device ID (может требоваться серверами для идентификации слотов)
- `-listen ADDR` - Изменить локальный UDP адрес (default: 127.0.0.1:9000)

## Конфигурация

Профили хранятся в `~/.config/qwdtt/profiles/`.
Device ID хранится в `~/.config/qwdtt/device_id

## Требования

- Linux с WireGuard kernel module
- Kernel module `wireguard`
- `iproute2` (команда `ip`)
- `wireguard-tools` (команда `wg`)
- `cap_net_admin` capabilities для работы без sudo

## Как это работает

1. Ядро клиента подключается к TURN
2. Проходит аутентификацию
3. Проходит авторизацию wdtt-сервера
4. Получает конфиг WireGuard
5. CLI создаёт WireGuard интерфейс `wg-qwdtt` через kernel module
6. Настраивается IP-адрес и MTU
7. Добавляются маршруты:
   - Исключения для TURN-серверов (через original gateway)
   - Split-route через WireGuard (0.0.0.0/1 + 128.0.0.0/1)

## Безопасность

Программа использует Linux capabilities вместо полного root доступа:
- `cap_net_admin+eip` на `qwdtt-cli` - управление сетевыми интерфейсами и маршрутами


## Лицензия

GNU GPL-3.0

## Структура проекта

```
.
├── main.go              # Точка входа
├── cli.go               # CLI команды и логика профилей
├── wireguard_linux.go   # WireGuard интеграция (kernel module)
├── go_client/           # Core библиотека андроид qWDTT
│   └── core/            # TURN, DTLS логика
├── flake.nix            # Nix flake конфигурация
├── go.mod               # Go dependencies
└── README.md            # Вы всё ещё здесь
```
