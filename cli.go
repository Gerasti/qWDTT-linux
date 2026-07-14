package main

import (
	"fmt"
	"os"
)

const version = "0.0.2"

func printUsage() {
	fmt.Printf(`qwdtt-cli v%s - VPN client через TURN-серверы VK

Usage:  qwdtt-cli [OPTIONS] COMMAND

Управление профилями:
  add <name> <wdtt://...>     Добавить новый профиль
  edit <name> [флаги]         Редактировать существующий профиль
  remove <name>               Удалить профиль (alias: rm)
  list                        Показать все профили (alias: ls)
  show <name>                 Показать детали профиля (alias: sh)
  enable <name>               Включить профиль (alias: en)
  disable <name>              Отключить профиль (alias: dis)

Подключение:
  connect [profile] [флаги]   Подключиться к VPN (alias: con)
                              Если профиль не указан, будет интерактивный выбор
                              Отключенные профили можно использовать явно указав имя
  disconnect                  Отключиться от VPN (alias: discon)
	  debug                       Показать отладочную информацию о текущем подключении (например, watch -n 1 qwdtt-cli debug)

Управление Device ID:
  device-id [id]              Показать или установить Device ID (alias: id)
  regenerate-id               Сгенерировать новый Device ID

Общие команды:
  version                     Показать версию
  help                        Показать это сообщение

Flags connect:
  -workers N                  Number of workers, multiple of 9 (default: 9)
  -mtu N                      Tunnel MTU (default: 1280, max: 1500)
  -hashes H1,H2               Override profile VK hashes
  -dns RESOLVER               DNS resolver (default: yandex)
                              Options: yandex, cloudflare, google,
                              doh-yandex, doh-cloudflare, doh-google,
                              custom:8.8.8.8:53,1.1.1.1:53
                              doh:https://dns.example.com/dns-query
  -auto-switch                Auto-switch to other profiles on failure
                              (uses enabled profiles only)
  -timeout N                  Timeout for -auto-switch in seconds (default: 120)

Флаги edit:
  -peer ADDR                  Изменить адрес сервера (IP:PORT)
  -password PASS              Изменить пароль
  -hashes H1,H2               Изменить VK-хеши
  -device-id ID               Изменить Device ID
  -listen ADDR                Изменить локальный UDP адрес (default: 127.0.0.1:9000)
  -priority N                 Установить приоритет профиля (выше = раньше при -auto-switch)

Примеры:
  qwdtt-cli add myserver wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2#MyServer
  qwdtt-cli con                        # интерактивный выбор профиля
  qwdtt-cli con myserver               # подключиться к профилю
  qwdtt-cli con myserver -auto-switch  # с автопереключением при неудаче
  qwdtt-cli debug                      # показать статистику текущего подключения
  qwdtt-cli discon                     # отключиться от VPN
  qwdtt-cli dis myserver               # отключить профиль (alias для disable)
  qwdtt-cli con disabled-profile       # можно подключиться явно указав имя
  qwdtt-cli en myserver                # включить профиль (alias для enable)
  qwdtt-cli edit myserver -password newpass
  qwdtt-cli edit myserver -priority 100  # установить высокий приоритет
  qwdtt-cli ls
  qwdtt-cli sh myserver
`, version)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "connect", "con":
		connectCmd()
	case "disconnect", "discon":
		disconnectCmd()
	case "debug":
		debugCmd()
	case "add":
		addCmd()
	case "edit":
		editCmd()
	case "remove", "rm":
		removeCmd()
	case "list", "ls":
		listCmd()
	case "show", "sh":
		showCmd()
	case "enable", "en":
		enableCmd()
	case "disable", "dis":
		disableCmd()
	case "device-id", "id":
		deviceIDCmd()
	case "regenerate-id":
		regenerateIDCmd()
	case "version", "--version":
		fmt.Printf("qwdtt-cli v%s\n", version)
	case "help", "-h", "--help":
		printUsage()
	case "__complete_enabled":
		for _, name := range listProfileNames() {
			fmt.Println(name)
		}
	case "__complete_disabled":
		for _, name := range listDisabledProfileNames() {
			fmt.Println(name)
		}
	case "__complete_all":
		for _, name := range listAllProfileNames() {
			fmt.Println(name)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}
