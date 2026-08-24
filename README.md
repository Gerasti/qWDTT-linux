# qWDTT linux v1.1.0

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
- test --group qubit — автотестирование работы ВСЕХ профилей группы qubit
- subscription или sub для обновления профилей через HTTPs
- bl — редактор списка обхода туннеля (bypass routes)
- Debug режим для мониторинга соединения

## Установка
### NixOS Module
<details>
<summary> show </summary>

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
    # wrappers.enable = true;  # defaults to true
    # wrappers.group = "users";  # a group that can run wrapped binaries
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
</details>

## Зависимости установки

### Debian/Ubuntu

```bash
sudo apt install iputils-ping curl
```

### Arch Linux

```bash
sudo pacman -S iputils curl
```

### Fedora

```bash
sudo dnf install iputils curl
```

#### До v1.0.0 также
```
patchelf
```

### Скачивание и установка бинарника из Release

```bash
# Скачать бинарник из Release
curl -L -o qwdtt https://github.com/Gerasti/qWDTT-linux/releases/download/v1.1.0/qwdtt

# Указать правильный интерпретатор (glibc) (до v1.0.0) 
patchelf --set-interpreter /lib64/ld-linux-x86-64.so.2 qwdtt

# Сделать исполняемым
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
wget https://go.dev/dl/go1.27.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.27.0.linux-amd64.tar.gz
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
<details>
<summary> show </summary>

```bash
# Добавить профиль (wdtt:// ссылка)
qwdtt add myserver wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2

# Добавить профиль (qwdtt://config ссылка — более гибкий формат с query-параметрами)
# ВАЖНО: для bash обязательно закавычивайте URL с & — иначе bash разобьёт команду
qwdtt add "qwdtt://config?name=МойСервер&peer=1.2.3.4:56000&hashes=hash1,hash2&workers=18&port=9000&pass=secret"

# Добавить профиль по qwdtt://config ссылке (имя берётся из параметра name)
qwdtt add 'qwdtt://config?name=МойСервер&peer=1.2.3.4:56000&hashes=hash1,hash2&workers=18&port=9000&pass=secret'

# Импорт профилей из JSON или ZIP (например, экспорт из мобильного клиента)
qwdtt import /path/to/profiles.json
qwdtt import /path/to/profiles.zip
qwdtt import profiles.json --dry-run      # просмотр без сохранения

# Подключиться
qwdtt con myserver

# Автопереключение профилей с режимом Raw
qwdtt con -auto-switch --mode raw

# С кастомным DNS resolver
qwdtt con myserver -dns doh-cloudflare
qwdtt con myserver -dns custom:8.8.8.8:53,1.1.1.1:53
qwdtt con myserver -dns doh:https://dns.example.com/dns-query

# Debug информация о подключении
qwdtt debug
# или watch -n 1 qwdtt debug

# Тестирование работоспособности
qwdtt test myserver

# Просмотр лога профиля (последние 20 строк)
qwdtt log estoniya -n 20

# Просмотр лога в реальном времени
qwdtt log autoswitch -f

# Live лог при подключении
qwdtt con -auto-switch -log

# SOCKS5 режим с доступом извне (0.0.0.0 вместо 127.0.0.1)
qwdtt con --mode socks --pub
qwdtt con --mode socks --public --socks-port 9051

# Отключить текущий профиль autoswitch (переключится на следующий)
qwdtt discon <current-profile-name>

# Отключиться
qwdtt disconnect (выбор, если несколько)

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
qwdtt disable -sub                     # отключить все профили подписок
qwdtt rm myserver mysrv2              # удалить несколько профилей
qwdtt show myserver mysrv2            # показать несколько профилей
qwdtt move myserver myserver-new      # переименовать профиль
qwdtt share myserver        # QR-код и share-ссылка (wdtt:// формат)
qwdtt share myserver -qwdtt  # QR-код и qwdtt://config? ссылка (alias: -q)
qwdtt share -group work      # QR-код и share-ссылка для всех профилей группы "work"
qwdtt share myserver | tail -n1 | wl-copy  # скопировать share-ссылку в буфер обмена
```
</details>

## Команды

```
qwdtt connect <profile> [флаги]      - Подключиться к VPN (alias: con; без профиля — интерактивный выбор)
qwdtt disconnect [profile]           - Отключиться от VPN (alias: discon; без профиля — отключить активный)
qwdtt log [profile] [-n N] [-f]      - Показать лог демона (alias: lg; без профиля — autoswitch или активный)
qwdtt share <name> [-qwdtt|-q] [-group GROUP] - Показать share-ссылку и QR-код (-qwdtt/-q: qwdtt://config? формат)
qwdtt debug                          - Показать debug информацию о соединении (alias: deb)
qwdtt test [profile1 or link...] [флаги]    - Тестировать подключение (VKAuth, Workers, Connect, InternetCheck; без аргументов — все профили)
                                       Аргументы: имена профилей, маски, wdtt:// и qwdtt:// ссылки
qwdtt add <name> <wdtt://... или "qwdtt://config?name=..."> - Добавить профиль
qwdtt edit <name1> [name2] ... [флаги]  - Редактировать профили (флаги применяются ко всем)
qwdtt move <old_name> <new_name>      - Переименовать профиль (alias: mv)
qwdtt remove <name1> [name2] ...     - Удалить профили (alias: rm, запрашивает подтверждение, -y/-yes — без него)
qwdtt list [<group1> ...] [флаги]    - Список профилей, отфильтрованный по группам (alias: ls)
qwdtt show <name1> [name2] ...       - Показать профили (alias: sh, -sub: профили подписок)
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
qwdtt import <file.json|file.zip> [--dry-run] - Импортировать профили из JSON или ZIP (--dry-run: просмотр без сохранения)
qwdtt bl <add|list|remove|find|init|load|unload> [-file PATH] [-p PROFILE] [-r] - Управление списком обхода туннеля (bypass routes) в JSON-файле
  bl list (ls)             - Показать все домена из файла (отметки [!] и [-] для неприменённых/удалённых)
  bl add <d1> [d2...] [-r] - Добавить домена/IP в файл (-r: hot-reload без переподключения)
  bl remove (rm) <d1> [d2...] [-y] [-r] - Удалить домена из файла (-y: без подтверждения, -r: hot-reload)
  bl find (fd) <d1> [d2...] - Проверить, есть ли домена в файле
  bl init [PATH]           - Создать новый bl-file (по умолчанию: qwdtt_bl.json в текущей директории)
  bl load <path>           - Подменить/включить bl-file для уже запущенного tun/raw/socks соединения БЕЗ переподключения
                             (домены сливаются с -bl; -file/-f PATH обязательны для add/remove/find/list/init)
  bl unload [-p PROFILE]   - Hot-reload: использовать только inline -bl домены, отбросить bl-file
                             (без переподключения; без флагов — автоопределение TUN/RAW профиля; -p для socks)
  Без -file/-p/--profile: bl add/remove/list/find/load/unload автоопределяют
  bl-file текущего запущенного профиля (TUN/RAW); для socks указывайте -p/--profile
qwdtt subscription <add|remove|show|move|update> [флаги] - Управление подписками (alias: sub)
  add <url>                    - Добавить подписку (имя берётся из JSON subscriptionName)
  add <name> <url>             - Добавить подписку с явным именем
  remove <name> [-y]           - Удалить подписку и все её профили (alias: rm)
  show [<name>]                - Показать подписку или список всех (alias: sh)
  move <old> <new>             - Переименовать подписку (alias: mv)
  update [<name>] [-y]         - Обновить профили из подписки (перезаписывает все профили)
qwdtt device-id [id]                 - Показать/установить Device ID (alias: id)
qwdtt regenerate-id                  - Перегенерировать Device ID
qwdtt version                        - Версия (alias: --version)
qwdtt help                           - Показать справку (alias: -h, --help)
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
mv     - move
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
- `-socks-user USER` - логин SOCKS5 (только с `-mode socks`)
- `-socks-password PASS` - пароль SOCKS5 (только с `-mode socks`)
- `-pub` / `--public` - слушать на 0.0.0.0 вместо 127.0.0.1 (только с `-mode socks`)
- `-raw-port PORT` - порт для raw TUN режима (default: 56003, требуется с `-mode raw`)
- `-transport TRANSPORT` - транспорт до TURN-relay: `udp` или `tcp` (default: udp). Использовать `tcp`, если UDP до TURN-relay блокируется
- `-log` - выводить лог демона в терминал в реальном времени
- `-bl` / `--black-list` - обход туннеля для указанных доменов/IP/CIDR: они идут напрямую, остальное — через туннель (режимы `tun`, `raw` и `socks`)
  - Через запятую, например: `-bl vk.ru,yandex.ru`
  - В режиме `socks` bypass работает на уровне SOCKS5-прокси: запросы к этим доменам/IP идут напрямую, не через туннель
- `-bl-file PATH` / `--black-list-file` - прочитать домены из JSON-файла (поле `bypassRoutes`), можно комбинировать с `-bl`
  - Пример: `-bl-file ./qwdtt_bypass_sites.json`

## Редактор bypass-списка (`qwdtt bl`)

<details>
<summary> show </summary>

Управляет списком обхода туннеля (поле `bypassRoutes`) в JSON-файле — тем же, что используется с флагом `-bl-file`. Работает как с обычным JSON-файлом, так и с ZIP (редактирование только файла, ZIP только для чтения).

```bash
# Показать все домены из файла (отметки [!] и [-] для неприменённых/удалённых)
qwdtt bl list -file ./qwdtt_bypass_sites.json      # alias: ls

# Добавить домены/IP (при необходимости файл создастся)
qwdtt bl add vk.ru yandex.ru -file ./qwdtt_bypass_sites.json
# -r: применить изменения на лету без переподключения
qwdtt bl add vk.ru yandex.ru -file ./qwdtt_bypass_sites.json -r

# Удалить домены (запрашивает подтверждение, -y — без него)
qwdtt bl rm vk.ru -file ./qwdtt_bypass_sites.json
# -r: применить удаление на лету
qwdtt bl rm vk.ru -file ./qwdtt_bypass_sites.json -r -y

# Проверить, есть ли домены в файле
qwdtt bl find yandex.ru -file ./qwdtt_bypass_sites.json   # alias: fd

# Подменить bl-file у уже запущенного соединения (tun/raw/socks) без переподключения
qwdtt con myserver -bl-file ./qwdtt_bypass_sites.json
qwdtt bl load ./qwdtt_bypass_sites_2.json              # заменить bl-file на лету
qwdtt debug                                           # увидеть новые splitroutes

# Отключить bl-file для запущенного соединения: использовать только inline -bl домены
qwdtt bl unload                                       # автоопределение TUN/RAW
qwdtt bl unload -p socks-profile                      # для socks профиля
```

- `-file PATH` (или `-f`) обязателен для add/remove/find/list/init. Поддерживаются `~` и переменные окружения (`$ENV`) в пути.
- `-p PROFILE` (или `-p`) для целевого socks-профиля; без него bl-file автоопределяется для TUN/RAW.
- `-r` / `--reload` — применить изменения на лету (hot-reload) без переподключения.
- `-y` — пропустить подтверждение при удалении.
- Формат поля `bypassRoutes` (массив или строка с переносами) сохраняется при редактировании; для нового файла создаётся заголовок `qwdtt-bypass`.
- IDN-домены (втб.рф) хранятся в punycode (xn--), а при выводе (`list`) показываются в Unicode.
- `list` помечает неприменённые домены `[!]` и удалённые без `-r` — `[-]`.
</details>

## Флаги list

- `-en` / `-enabled` - показать только включённые профили
- `-dis` / `-disabled` - показать только отключённые профили
- `-ro` - показать только read-only профили
- `-sub` - показать только профили, управляемые подписками
- `-active` / `-A` - показать только запущенные (активные) профили
- `-no-ip` - не показывать IP (адрес сервера) профилей
- `<group1> [group2] ...` - позиционный аргумент: показать только профили из указанных групп (поддерживается несколько)

  Примеры: `qwdtt ls work`, `qwdtt ls work personal`, `qwdtt ls -ro`, `qwdtt ls -en work`, `qwdtt ls -A`, `qwdtt ls -no-ip`

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
- `-sub` - только профили, управляемые подписками

## Флаги test

- `-ro` - тестировать только read-only профили
- `-en` / `-enabled` - тестировать только включённые профили
- `-dis` / `-disabled` - тестировать только отключённые профили
- `-group GROUP` - тестировать все профили группы
- `-sub` - тестировать все профили, управляемые подписками
- `-mode MODE` - режим подключения: `tun`, `socks` или `raw` (default: tun)
- `-socks-port PORT` - порт SOCKS5 (default: 9050, с `-mode socks`)
- `-socks-user USER` - логин SOCKS5 (только с `-mode socks`)
- `-socks-password PASS` - пароль SOCKS5 (только с `-mode socks`)
- `-transport TRANSPORT` - транспорт до TURN-relay: `udp` или `tcp` (default: udp)
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
- MSS clamp выполняется через nftables (GO библиотека `google/nftables)
- Флаг `-raw-port PORT` задаёт порт для обратного соединения (default: 56003)

## Форматы ссылок
<details>
<summary> show </summary>

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
- `hashes` — VK хеши через запятую (один обязателен)
- `workers` — количество воркеров (по умолчанию 9)
- `port` — локальный порт прослушивания (по умолчанию 9000)
- `pass` — пароль (обязательно)

Оба формата поддерживаются в командах `add`, `test` и для `share -qwdtt` (alias `-q`).

> **Важно для bash**: ссылка **qwdtt://config?** содержит символы `&`, которые bash интерпретирует как оператор фонового выполнения. **Обязательно закавычивайте** URL в кавычки:
> ```bash
> # Правильно (одинарные кавычки):
> qwdtt add 'qwdtt://config?name=МойСервер&peer=1.2.3.4:56000&pass=secret'
> qwdtt test 'qwdtt://config?name=МойСервер&peer=1.2.3.4:56000&pass=secret'
> ```
> В fish или zsh эта проблема отсутствует.
</details>

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
  "subscriptionName": "BLUA VPN",
  "description": "Подписка · до 24.08.2026",
  "profiles": [
    {
      "name": "FREELAND",
      "peer": "121.11.142.10:56000",
      "password": "P@ssw0rd",
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
| `qwdtt sub move <old> <new>` | Переименовать подписку (alias: mv) |
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

### Примеры

```bash
# Добавить подписку (имя из JSON)
qwdtt sub add https://example.com/sub.json -y

# Добавить подписку с явным именем
qwdtt sub add "My VPN" "https://example.com/sub.json"

# Показать все подписки
qwdtt sub show

# Обновить конкретную подписку
qwdtt sub update "Qubit VPN"

# Обновить все подписки
qwdtt sub update

# Удалить подписку и все её профили
qwdtt sub rm "Qubit VPN" -y
```

## DNS Resolvers

<details>
<summary> show </summary>
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
</details>

## Suspend/Resume

Автоматическое переподключение после пробуждения через systemd D-Bus. Работает без настройки на системах с systemd.

## Требования

- Linux
- `iputils` (ping), `curl`
- `cap_net_admin` capabilities (иначе будет доступен только socks)
- systemd (для suspend/resume)
- D-Bus session bus (для уведомлений)
- Версия ядра => 4.14 (MSS clamp, стабильность для режима Raw)

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
├── translit.go           # Транслитерация имён
├── url_parser.go         # Парсинг wdtt:// и qwdtt:// URL
├── wireguard_linux.go    # WireGuard интеграция (netlink/wgctrl, capabilities)
├── qwdtt_bypass_sites.json # Пример файла bypass-маршрутов (поле bypassRoutes)
├── internal/core/        # Core библиотека (TURN, DTLS, DoH, captcha, bypass и др.)
│   ├── core.go           # Ядро подключения
│   ├── session.go        # Сессии и автопереключение
│   ├── dispatcher.go     # Диспетчер пакетов
│   ├── tun_linux.go      # Raw TUN/TAP, маршруты, MSS clamp (nftables)
│   ├── bypass_routes_linux.go # Bypass-маршруты
│   ├── captcha_v2.go     # VK captcha (Go solver)
│   ├── captcha_v2_slider.go # VK captcha slider
│   ├── wireproxy_runner.go # SOCKS5 режим (wireproxy)
│   ├── doh.go            # DNS-over-HTTPS
│   ├── obfs.go           # Обфускация
│   ├── protocol.go       # Протокол обмена
│   ├── wgconfig.go       # WireGuard конфигурация
│   └── ...               # прочие модули
├── modules/nixos/        # NixOS module
│   └── default.nix       # NixOS security wrapper
├── completions/          # Bash/Fish автодополнения
├── flake.nix             # Nix flake конфигурация
├── go.mod                # Go dependencies
├── go.sum                # Контрольные суммы зависимостей
├── vendor/               # Вендорированные зависимости
└── LICENSE               # GNU GPL-3.0
```

## Лицензия

GNU GPL-3.0
