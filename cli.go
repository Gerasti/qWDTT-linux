package main

import (
	"fmt"
	"os"
)

const version = "0.9.5"

func printUsage() {
	fmt.Printf(`qwdtt v%s - VPN client via VK TURN servers

Usage:  qwdtt [OPTIONS] COMMAND

Profile Management:
  add <name> <wdtt://...>     Add a new profile
  edit <name1> [name2] ...    Edit existing profiles, same flags apply to all (alias: none)
                              -group GROUP: edit all profiles in the group
  remove <name1> [name2] ...  Remove profiles (alias: rm)
                               -group GROUP: remove all profiles in the group
                               -y/-yes: skip confirmation prompt
  move <old_name> <new_name>  Rename a profile (alias: mv)
  edit/remove/show/enable/disable/test also accept glob masks, e.g. rm 'wdtt_*'
                              (quote the mask to avoid shell globbing)
  list [<group1> ...] [flags] Show profiles, optionally filtered by group(s) (alias: ls)
                               -en/-enabled: enabled only
                               -dis/-disabled: disabled only
                               -ro: read-only profiles only
  show <name1> [name2] ...    Show profile details (alias: sh)
                              -group GROUP: show all profiles in the group
  share <name>                Show profile share link and QR code
  							   (e.g. qwdtt share <name> | tail -n1 | wl-copy)
  enable <name1> [name2] ...  Enable profiles (alias: en)
                              -group GROUP: enable all profiles in the group
  disable <name1> [name2] ... Disable profiles (alias: dis)
                              -group GROUP: disable all profiles in the group
  import <file>               Import profiles from JSON or ZIP file
                               -dry-run: show what would be imported without saving

Connection:
  connect [profile] [flags]   Connect to VPN (alias: con)
                              If profile is not specified, interactive selection
                              Disabled profiles can be used by explicitly specifying name
  disconnect [profile]        Disconnect from VPN (alias: discon)
                              If profile is not specified, disconnects active profile
  log [profile] [-n N] [-f]   Show daemon log file (default: autoswitch or active) (alias: lg)
                               -n N: show last N lines; -f: follow in real-time
  debug                       Show debug information about current connection(s) (alias: deb)
                               (e.g., watch -n 1 qwdtt debug)
  test [profile1 ...] [--ro] [--enabled] [--disabled] [--group GROUP]
                                Test profile(s) connectivity (VKAuth, Workers, Connect, InternetCheck)
                                Without args: test all profiles
                                Each arg can be a profile name or wdtt:// link
                                -ro: test only read-only profiles
                               -enabled/-en: test only enabled profiles
                               -disabled/-dis: test only disabled profiles
                               -group GROUP: test all profiles in the group
                               -mode tun|socks: connection mode (default: tun)
                               -socks-port N: SOCKS5 port (default: 9050, with -mode socks)
                               -timeout N: connection timeout in seconds (default: 10)
                               -delay N: pause between profiles in seconds (default: 5)

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
                               Options: auto, rjs, wv
                               auto - Go solver with WebView fallback
                               rjs  - pure Go solver only
                               wv   - external WebView solver (via CAPTCHA_SOLVE protocol)
  -auto-switch                Auto-switch to other profiles on failure
                                (uses enabled profiles only)
  -timeout N                  Timeout for -auto-switch in seconds (default: 120)
  -mode MODE                  Connection mode (default: tun)
                                Options: tun - direct tun WireGuard
                                         socks - local SOCKS5 proxy
                                         raw - raw IP without WireGuard (server -listen-raw)
  -socks-port PORT            SOCKS5 port (default: 9050)
                                  Required with -mode socks
  -raw-port PORT              Raw mode server port (default: 56003)
                                  Only with -mode raw
  -log                        Show daemon log output in terminal in real-time
  -toggle                     Stop running profile, or start if not running
  -bl DOMAINS or IP, --black-list   These domains go direct; everything else goes through tunnel
                                Comma-separated, e.g. -bl vk.ru,yandex.ru
  -bl-file PATH, --black-list-file  Read domains from JSON file (bypassRoutes field). Can combine with -bl
                                e.g. -bl-file ./qwdtt_bypass_sites.json

Edit Flags:
  -peer ADDR                  Change server address (IP:PORT)
  -password PASS              Change password
  -hashes H1,H2               Change VK hashes
  -device-id ID               Change Device ID
  -listen ADDR                Change local UDP address (default: 127.0.0.1:9000)
  -priority N                 Set profile priority (higher = earlier with -auto-switch)
  -groups G1,G2               Set profile groups (comma-separated, "" or none to clear)

Examples:
  qwdtt add myserver wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2#MyServer
  qwdtt con                        # interactive profile selection
  qwdtt con myserver               # connect to profile
  qwdtt con myserver --toggle      # with stop if run, start if not
  qwdtt con myserver -auto-switch  # with auto-switching on failure
  qwdtt con -auto-switch -log      # start autoswitch with live log output
  qwdtt con --mode socks           # SOCKS5 mode with default port 9050
  qwdtt con --mode socks --socks-port 9051 # SOCKS5 with 9051 port
  qwdtt debug                      # show current connection stats
  qwdtt discon                     # disconnect active profile
  qwdtt discon myserver            # disconnect specific profile
  qwdtt discon wlrus-n2u2          # with autoswitch: switch to next profile
  qwdtt dis myserver               # disable profile (alias for disable)
  qwdtt con disabled-profile       # can connect by explicitly specifying name
  qwdtt en myserver                # enable profile (alias for enable)
  qwdtt edit myserver -password newpass
  qwdtt edit myserver mysrv2 -priority 100   # edit multiple profiles at once
  qwdtt edit mysrv -priority 100   # set high priority
  qwdtt edit myserver mysrv -groups work # set group myserver, mysrv to group "work"
  qwdtt move myserver myserver-rename     # rename profile (alias: mv)
  qwdtt ls work                    # show profiles in group "work"
  qwdtt ls work personal           # show profiles in either group
  qwdtt ls -ro                     # show only read-only profiles
  qwdtt ls -en                     # show only enabled profiles
  qwdtt dis myserver mysrv2        # disable multiple profiles
  qwdtt en myserver mysrv2         # enable multiple profiles
  qwdtt rm myserver mysrv2         # remove multiple profiles
  qwdtt rm 'wdtt_*'                # remove all profiles matching the mask (asks confirmation)
  qwdtt rm 'wdtt_*' -y             # remove matching profiles without confirmation
  qwdtt test 'wdtt_*'              # test all profiles matching the mask
  qwdtt show myserver mysrv2       # show details of multiple profiles
  qwdtt test myserver mysrv2       # test multiple profiles
  qwdtt en -group work             # enable all profiles in group "work"
  qwdtt dis -group work            # disable all profiles in group "work"
  qwdtt edit 'wdtt-*' -group work --groups test # move wdtt-* profiles and group "work" to group "test"
  qwdtt edit -group work -priority 100   # edit all profiles in group "work"
  qwdtt test -group work           # test all profiles in group "work"
  qwdtt show -group work           # show all profiles in group "work"
  qwdtt rm -group work             # remove all profiles in group "work"
  qwdtt log autoswitch -n 20       # show last 20 log lines
  qwdtt log autoswitch -f          # follow log in real-time
  qwdtt sh myserver -group work    # show settings of myserver and group "work"
  qwdtt test                       # test all profiles
  qwdtt test --mode socks --group test --delay 6 # test profile test0
  qwdtt test myserver -group work  # test specific profile and group "work"
  qwdtt test --ro                  # test all read-only profiles
  qwdtt test --timeout 15          # set timeout to 15 seconds
  qwdtt test --enabled             # test only enabled profiles
  qwdtt test --disabled            # test only disabled profiles
  qwdtt test --mode socks          # test in SOCKS5 mode without setcap and root
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
	case "debug", "deb":
		debugCmd()
	case "add":
		addCmd()
	case "edit":
		editCmd()
	case "remove", "rm":
		removeCmd()
	case "move", "mv":
		moveCmd()
	case "list", "ls":
		listCmd()
	case "show", "sh":
		showCmd()
	case "share":
		shareCmd()
	case "enable", "en":
		enableCmd()
	case "disable", "dis":
		disableCmd()
	case "import":
		importCmd()
	case "device-id", "id":
		deviceIDCmd()
	case "regenerate-id":
		regenerateIDCmd()
	case "log", "lg":
		logCmd()
	case "test":
		testCmd()
	case "version", "--version":
		fmt.Printf("qwdtt v%s\n", version)
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
	case "__complete_logs":
		for _, name := range listLogProfileNames() {
			fmt.Println(name)
		}
	case "__complete_groups":
		seen := make(map[string]bool)
		for _, name := range listAllProfileNames() {
			prof, err := loadProfile(name)
			if err != nil {
				continue
			}
			for _, g := range prof.Groups {
				if !seen[g] {
					seen[g] = true
					fmt.Println(g)
				}
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}
