# Fish completion for qwdtt

# Helper function to get enabled profile names
function __qwdtt_profiles
    qwdtt __complete_enabled 2>/dev/null
end

# Helper that returns enabled profiles which are NOT currently running
function __qwdtt_available_connect_profiles
    set -l enabled (qwdtt __complete_enabled 2>/dev/null)
    set -l running (qwdtt __complete_running 2>/dev/null)
    for p in $enabled
        contains $p $running; or printf '%s\n' $p
    end
end

# Helper function to get all profile names
function __qwdtt_all_profiles
    qwdtt __complete_all 2>/dev/null
end

# Helper function to get profile names that have existing log files
function __qwdtt_log_profiles
    qwdtt __complete_logs 2>/dev/null
end

# Helper function to get disabled profile names
function __qwdtt_disabled_profiles
    qwdtt __complete_disabled 2>/dev/null
end

# Helper function to get non-read-only (user) profile names
function __qwdtt_user_profiles
    qwdtt __complete_user 2>/dev/null
end

# Helper function to get group names
function __qwdtt_groups
    qwdtt __complete_groups 2>/dev/null
end

# Helper function to get subscription names
function __qwdtt_subscriptions
    qwdtt __complete_subscriptions 2>/dev/null
end

# Check if the last token on the command line is a group flag (-group or --group)
function __qwdtt_last_is_group_flag
    set -l cmd (commandline -opc)
    test (count $cmd) -ge 2; and test "$cmd[-1]" = "--group" -o "$cmd[-1]" = "-group"
end

# Check if the last token is NOT a flag (doesn't start with -)
function __qwdtt_last_is_not_flag
    set -l cmd (commandline -opc)
    test (count $cmd) -ge 1; and not string match -q -- "-*" $cmd[-1]
end

# Check if the last token equals any of the given arguments
function __qwdtt_last_is
    set -l cmd (commandline -opc)
    test (count $cmd) -ge 1; and contains -- $cmd[-1] $argv
end

# Helper to check if we're completing after a specific command (including aliases)
function __qwdtt_seen_command
    set -l cmd (commandline -opc)
    if test (count $cmd) -ge 2
        set -l subcmd $cmd[2]
        for arg in $argv
            if test "$subcmd" = "$arg"
                return 0
            end
        end
    end
    return 1
end

# Check if no non-flag positional argument has been given yet after the command
function __qwdtt_no_positional_arg
    set -l cmd (commandline -opc)
    for w in $cmd[3..]
        string match -q -- "-*" $w; or return 1
    end
    return 0
end

# Main commands (only primary commands, no aliases in completion list)
complete -c qwdtt -f
complete -c qwdtt -n __fish_use_subcommand -a connect -d "Connect to VPN"
complete -c qwdtt -n __fish_use_subcommand -a disconnect -d "Disconnect from VPN"
complete -c qwdtt -n __fish_use_subcommand -a switch -d "Switch to next profile in auto-switch mode"
complete -c qwdtt -n __fish_use_subcommand -a debug -d "Show debug information"
complete -c qwdtt -n __fish_use_subcommand -a add -d "Add profile"
complete -c qwdtt -n __fish_use_subcommand -a edit -d "Edit profile"
complete -c qwdtt -n __fish_use_subcommand -a move -d "Rename profile"
complete -c qwdtt -n __fish_use_subcommand -a remove -d "Remove profile"
complete -c qwdtt -n __fish_use_subcommand -a list -d "List profiles"
complete -c qwdtt -n __fish_use_subcommand -a show -d "Show profile"
complete -c qwdtt -n __fish_use_subcommand -a share -d "Show profile share link and QR code"
complete -c qwdtt -n __fish_use_subcommand -a enable -d "Enable profile"
complete -c qwdtt -n __fish_use_subcommand -a disable -d "Disable profile"
complete -c qwdtt -n __fish_use_subcommand -a import -d "Import profiles from JSON"
complete -c qwdtt -n __fish_use_subcommand -a bl -d "Manage bypass routes file domains"
complete -c qwdtt -n __fish_use_subcommand -a subscription -d "Manage subscriptions"
# bl subcommands (offered right after 'qwdtt bl')
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a "init add list remove find load unload ls rm fd" -d "bl subcommand"
complete -c qwdtt -n "__qwdtt_seen_command bl" -s f -l f -s file -l file -r -d "Path to bypass routes JSON file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = remove" -s y -l y -d "Skip confirmation prompt (remove only)"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = init; and __qwdtt_last_is_not_flag" -a "(__fish_complete_path (commandline -t))" -d "Path to new bypass routes JSON file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = load; and __qwdtt_last_is_not_flag" -a "(__fish_complete_path (commandline -t))" -d "Path to bypass routes JSON file"
complete -c qwdtt -n "__qwdtt_seen_command bl" -s p -l p -s profile -l profile -d "Target a specific running profile's bl-file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and __qwdtt_last_is -p --p -profile --profile" -f -a "(__qwdtt_running_profiles)" -d "Running profile"
complete -c qwdtt -n "__qwdtt_seen_command bl" -s r -l r -s reload -l reload -d "Hot-reload bl-file to the running daemon after changes (tun/raw/socks)"
# import: file path + dry-run flag
complete -c qwdtt -n "__qwdtt_seen_command import" -a "(__fish_complete_path (commandline -t))" -d "Path to profile JSON or ZIP file"
complete -c qwdtt -n "__qwdtt_seen_command import" -s dry-run -l dry-run -d "Show what would be imported without saving"
complete -c qwdtt -n __fish_use_subcommand -a bl -d "Manage bypass routes file domains"
# bl subcommands (individual, only at subcommand position)
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a add -d "Add domains to bypass file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a list -d "List bypass route domains"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a remove -d "Remove domains from bypass file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a find -d "Check domains in bypass file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a init -d "Create new bypass routes file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a load -d "Hot-reload bl-file for running connection"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a unload -d "Hot-reload keeping inline -bl domains only"
complete -c qwdtt -n __fish_use_subcommand -a device-id -d "Show/set Device ID"
complete -c qwdtt -n __fish_use_subcommand -a regenerate-id -d "Regenerate Device ID"
complete -c qwdtt -n __fish_use_subcommand -a log -d "Show daemon log file"
complete -c qwdtt -n __fish_use_subcommand -a test -d "Test profile connectivity"
complete -c qwdtt -n __fish_use_subcommand -a version -d "Show version"
complete -c qwdtt -n __fish_use_subcommand -a help -d "Show help"

# connect/con - profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command connect con; and __qwdtt_no_positional_arg; and __qwdtt_last_is_not_flag" -f -a "(__qwdtt_available_connect_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s workers -l workers -d "Number of workers"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s mtu -l mtu -d "Tunnel MTU"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s hashes -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s dns -l dns -d "DNS resolver"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -s captcha -l captcha -d "Captcha bypass mode" -a "auto rjs"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s timeout -l timeout -d "Connection timeout (seconds)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s auto-switch -l auto-switch -d "Auto-switch on failure"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -s mode -l mode -d "Connection mode" -a "tun socks raw"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s socks-port -l socks-port -d "SOCKS5 port (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s socks-user -l socks-user -d "SOCKS5 username (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s socks-password -l socks-password -d "SOCKS5 password (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s pub -l pub -l public -d "Listen on 0.0.0.0 instead of 127.0.0.1 (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s raw-port -l raw-port -d "Raw mode server port (only with -mode raw)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -s transport -l transport -d "Transport to TURN relay" -a "udp tcp"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s toggle -l toggle -d "Stop running profile, or start if not running"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s black-list -l black-list -a "bl" -d "These domains go direct"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s bl -l bl -d "Alias for --black-list"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -s black-list-file -l black-list-file -a "(__fish_complete_path (commandline -t))" -d "Read blacklist domains from JSON file"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -s bl-file -l bl-file -a "(__fish_complete_path (commandline -t))" -d "Alias for --black-list-file"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s log -l log -d "Show daemon log output in terminal in real-time"

# show - all profile names and -group flag
complete -c qwdtt -n "__qwdtt_seen_command show sh; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command show sh" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command show sh" -s sub -l sub -d "Show all profiles managed by any subscription"

# share - all profile names, -group flag, -qwdtt flag
complete -c qwdtt -n "__qwdtt_seen_command share; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command share" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command share" -f -s qwdtt -l qwdtt -d "Generate qwdtt://config? URL format instead of wdtt://"
complete -c qwdtt -n "__qwdtt_seen_command share" -f -s q -l q -d "Alias for -qwdtt"

# remove/rm - all profile names, -group flag, confirmation bypass
complete -c qwdtt -n "__qwdtt_seen_command remove rm; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -f -s y -l y -s yes -l yes -d "Skip confirmation prompt"

# move/mv - all profile names for both arguments
complete -c qwdtt -n "__qwdtt_seen_command move mv" -f -a "(__qwdtt_all_profiles)" -d "Profile"

# disconnect/discon - running profile names
function __qwdtt_running_profiles
    qwdtt __complete_running 2>/dev/null
end

complete -c qwdtt -n "__qwdtt_seen_command disconnect discon; and __qwdtt_no_positional_arg; and __qwdtt_last_is_not_flag" -f -a "(__qwdtt_running_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command disconnect discon" -f -s all -l all -d "Отключить все запущенные профили"
complete -c qwdtt -n "__qwdtt_seen_command disconnect discon" -f -s y -l y -s yes -l yes -d "Skip confirmation prompt (with --all)"

# enable/en - only disabled profiles and -group flag
complete -c qwdtt -n "__qwdtt_seen_command enable en; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_disabled_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -f -s ro -l ro -d "Only operate on read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -s sub -l sub -d "Operate on all profiles managed by any subscription"

# disable/dis - only enabled profiles and -group flag
complete -c qwdtt -n "__qwdtt_seen_command disable dis; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -f -s ro -l ro -d "Only operate on read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -s sub -l sub -d "Operate on all profiles managed by any subscription"

# edit - all profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command edit; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s peer -l peer -d "Server address (IP:PORT)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s password -l password -d "Password"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s hashes -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s device-id -l device-id -d "Device ID"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s listen -l listen -d "Local address"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s priority -l priority -d "Profile priority"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s workers -l workers -d "Worker count (must be multiple of 9)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s groups -l groups -d "Profile groups (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"

# add - existing non-read-only profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command add" -f -a "(__qwdtt_user_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command add" -f -s device-id -l device-id -d "Device ID"

# log/lg - profile names from existing log files and flags
complete -c qwdtt -n "__qwdtt_seen_command log lg" -f -a "(__qwdtt_log_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command log lg" -f -s n -l n -d "Number of lines to show"
complete -c qwdtt -n "__qwdtt_seen_command log lg" -f -s f -l f -s follow -l follow -d "Follow log in real-time"

# list/ls - optional group filter and flags
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -a "(qwdtt __complete_groups 2>/dev/null)" -d "Group"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -s en -l en -d "Show only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -s enabled -l enabled -d "Show only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -s dis -l dis -d "Show only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -s disabled -l disabled -d "Show only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -s ro -l ro -d "Show only read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -s sub -l sub -d "Show only profiles managed by any subscription"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -s active -l active -d "Show only running profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -s A -l A -d "Show only running profiles (alias for --active)"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -s no-ip -l no-ip -d "Do not display profile server IPs (peer column)"

# test - profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command test; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s ro -l ro -d "Test only read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s en -l en -d "Test only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s enabled -l enabled -d "Test only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s dis -l dis -d "Test only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s disabled -l disabled -d "Test only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s group -l group -r -a "(__qwdtt_groups)" -d "Test all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command test" -s sub -l sub -d "Test all profiles managed by any subscription"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s timeout -l timeout -d "Connection timeout in seconds"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s mode -l mode -d "Connection mode" -a "tun socks raw"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s socks-port -l socks-port -d "SOCKS5 port"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s socks-user -l socks-user -d "SOCKS5 username (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s socks-password -l socks-password -d "SOCKS5 password (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s pub -l pub -l public -d "Listen on 0.0.0.0 instead of 127.0.0.1 (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s transport -l transport -d "Transport to TURN relay" -a "udp tcp"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s delay -l delay -d "Pause between profiles in seconds"

# subscription (sub) subcommands
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -eq 2" -f -a "add remove rm show sh move mv update upd" -d "subscription subcommand"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = add" -f -a "(__fish_complete_path)" -d "Subscription URL"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3" -a "(__qwdtt_subscriptions)" -d "Subscription name"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub" -s y -l y -s yes -l yes -d "Skip confirmation prompt"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = add" -f -l yes -d "Skip confirmation prompt"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = move; and test (count (commandline -opc)) -le 3" -a "(__qwdtt_subscriptions)" -d "Subscription name"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 4; and test (commandline -opc)[3] = move; and test (commandline -opc)[4] = mv" -a "(__qwdtt_subscriptions)" -d "Subscription name"
