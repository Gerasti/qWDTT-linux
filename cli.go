package main

import (
	"fmt"
	"os"
)

const version = "0.5.0"

func printUsage() {
	fmt.Printf(`qwdtt-cli v%s - VPN client via VK TURN servers

Usage:  qwdtt-cli [OPTIONS] COMMAND

Profile Management:
  add <name> <wdtt://...>     Add a new profile
  edit <name> [flags]         Edit an existing profile
  remove <name>               Remove a profile (alias: rm)
  list                        Show all profiles (alias: ls)
  show <name>                 Show profile details (alias: sh)
  enable <name>               Enable a profile (alias: en)
  disable <name>              Disable a profile (alias: dis)

Connection:
  connect [profile] [flags]   Connect to VPN (alias: con)
                              If profile is not specified, interactive selection
                              Disabled profiles can be used by explicitly specifying name
  disconnect                  Disconnect from VPN (alias: discon)
  debug                       Show debug information about current connection
                              (e.g., watch -n 1 qwdtt-cli debug)

Device ID Management:
  device-id [id]              Show or set Device ID (alias: id)
  regenerate-id               Generate a new Device ID

General Commands:
  version                     Show version
  help                        Show this message

Connect Flags:
  -workers N                  Number of workers, multiple of 9 (default: 9)
  -mtu N                      Tunnel MTU (default: 1280, max: 1500)
  -hashes H1,H2               Override profile VK hashes
  -dns RESOLVER               DNS resolver (default: yandex)
                              Options: yandex, cloudflare, google,
                              doh-yandex, doh-cloudflare, doh-google,
                              custom:8.8.8.8:53,1.1.1.1:53
                              doh:https://dns.example.com/dns-query
  -captcha MODE               Captcha bypass mode (default: auto)
                              Options: auto, rjs
                              auto - automatic mode (pure Go solver with retries)
                              rjs  - pure Go solver only
  -auto-switch                Auto-switch to other profiles on failure
                              (uses enabled profiles only)
  -timeout N                  Timeout for -auto-switch in seconds (default: 120)

Edit Flags:
  -peer ADDR                  Change server address (IP:PORT)
  -password PASS              Change password
  -hashes H1,H2               Change VK hashes
  -device-id ID               Change Device ID
  -listen ADDR                Change local UDP address (default: 127.0.0.1:9000)
  -priority N                 Set profile priority (higher = earlier with -auto-switch)

Examples:
  qwdtt-cli add myserver wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2#MyServer
  qwdtt-cli con                        # interactive profile selection
  qwdtt-cli con myserver               # connect to profile
  qwdtt-cli con myserver -captcha rjs  # connect with pure Go captcha solver
  qwdtt-cli con myserver -auto-switch  # with auto-switching on failure
  qwdtt-cli debug                      # show current connection stats
  qwdtt-cli discon                     # disconnect from VPN
  qwdtt-cli dis myserver               # disable profile (alias for disable)
  qwdtt-cli con disabled-profile       # can connect by explicitly specifying name
  qwdtt-cli en myserver                # enable profile (alias for enable)
  qwdtt-cli edit myserver -password newpass
  qwdtt-cli edit myserver -priority 100  # set high priority
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
