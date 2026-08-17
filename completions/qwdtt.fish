# Fish completion for qwdtt

# Helper function to get enabled profile names
function __qwdtt_profiles
    qwdtt __complete_enabled 2>/dev/null
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

# Main commands (only primary commands, no aliases in completion list)
complete -c qwdtt -f
complete -c qwdtt -n __fish_use_subcommand -a connect -d "Connect to VPN"
complete -c qwdtt -n __fish_use_subcommand -a disconnect -d "Disconnect from VPN"
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
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -eq 2" -f -a "add list remove find" -d "bl subcommand"
complete -c qwdtt -n "__qwdtt_seen_command bl" -l file -r -d "Path to bypass routes JSON file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = remove" -s y -l y -d "Skip confirmation prompt (remove only)"
# import: file path + dry-run flag
complete -c qwdtt -n "__qwdtt_seen_command import" -a "(__fish_complete_path)" -d "Path to profile JSON or ZIP file"
complete -c qwdtt -n "__qwdtt_seen_command import" -l dry-run -d "Show what would be imported without saving"
complete -c qwdtt -n __fish_use_subcommand -a bl -d "Manage bypass routes file domains"
# bl subcommands
complete -c qwdtt -n "__qwdtt_seen_command bl" -f -a add -d "Add domains to bypass file"
complete -c qwdtt -n "__qwdtt_seen_command bl" -f -a list -d "List bypass route domains"
complete -c qwdtt -n "__qwdtt_seen_command bl" -f -a remove -d "Remove domains from bypass file"
complete -c qwdtt -n "__qwdtt_seen_command bl" -f -a find -d "Check domains in bypass file"
complete -c qwdtt -n "__qwdtt_seen_command bl" -s f -l file -r -d "Path to bypass routes JSON file"
complete -c qwdtt -n "__qwdtt_seen_command bl; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = remove" -s y -l y -d "Skip confirmation prompt (remove only)"
# import: positional file path + dry-run flag
complete -c qwdtt -n "__qwdtt_seen_command import" -a "(__fish_complete_path)" -d "Path to profile JSON or ZIP file"
complete -c qwdtt -n "__qwdtt_seen_command import" -s dry-run -l dry-run -d "Show what would be imported without saving"
complete -c qwdtt -n __fish_use_subcommand -a device-id -d "Show/set Device ID"
complete -c qwdtt -n __fish_use_subcommand -a regenerate-id -d "Regenerate Device ID"
complete -c qwdtt -n __fish_use_subcommand -a log -d "Show daemon log file"
complete -c qwdtt -n __fish_use_subcommand -a test -d "Test profile connectivity"
complete -c qwdtt -n __fish_use_subcommand -a version -d "Show version"
complete -c qwdtt -n __fish_use_subcommand -a help -d "Show help"

# connect/con - profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -a "(__qwdtt_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l workers -d "Number of workers"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l mtu -d "Tunnel MTU"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l dns -d "DNS resolver"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l captcha -d "Captcha bypass mode" -a "auto rjs"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l timeout -d "Connection timeout (seconds)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l auto-switch -d "Auto-switch on failure"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l mode -d "Connection mode" -a "tun socks raw"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l socks-port -d "SOCKS5 port (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l socks-user -d "SOCKS5 username (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l socks-password -d "SOCKS5 password (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l raw-port -d "Raw mode server port (only with -mode raw)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l transport -d "Transport to TURN relay" -a "udp tcp"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l toggle -d "Stop running profile, or start if not running"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -l black-list -a "bl" -d "These domains go direct"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -s bl -l bl -d "Alias for --black-list"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -l black-list-file -a "(__fish_complete_path)" -d "Read blacklist domains from JSON file"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -f -r -s bl-file -l bl-file -a "(__fish_complete_path)" -d "Alias for --black-list-file"

# show - all profile names and -group flag
complete -c qwdtt -n "__qwdtt_seen_command show sh; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command show sh" -f -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command show sh" -s sub -l sub -d "Show all profiles managed by any subscription"

# share - all profile names, -group flag, -qwdtt flag
complete -c qwdtt -n "__qwdtt_seen_command share; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command share" -f -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command share" -f -s qwdtt -l qwdtt -d "Generate qwdtt://config? URL format instead of wdtt://"
complete -c qwdtt -n "__qwdtt_seen_command share" -f -s q -l q -d "Alias for -qwdtt"

# remove/rm - all profile names, -group flag, confirmation bypass
complete -c qwdtt -n "__qwdtt_seen_command remove rm; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -f -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -f -s y -l y -l yes -d "Skip confirmation prompt"

# move/mv - all profile names for both arguments
complete -c qwdtt -n "__qwdtt_seen_command move mv" -f -a "(__qwdtt_all_profiles)" -d "Profile"

# disconnect/discon - running profile names
function __qwdtt_running_profiles
    qwdtt __complete_running 2>/dev/null
end

complete -c qwdtt -n "__qwdtt_seen_command disconnect discon" -f -a "(__qwdtt_running_profiles)" -d "Profile"

# enable/en - only disabled profiles and -group flag
complete -c qwdtt -n "__qwdtt_seen_command enable en; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_disabled_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -f -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -f -l ro -d "Only operate on read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -s sub -l sub -d "Operate on all profiles managed by any subscription"

# disable/dis - only enabled profiles and -group flag
complete -c qwdtt -n "__qwdtt_seen_command disable dis; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -f -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -f -l ro -d "Only operate on read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -s sub -l sub -d "Operate on all profiles managed by any subscription"

# edit - all profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command edit; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l peer -d "Server address (IP:PORT)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l password -d "Password"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l device-id -d "Device ID"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l listen -d "Local address"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l priority -d "Profile priority"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l workers -d "Worker count (must be multiple of 9)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l groups -d "Profile groups (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -f -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"

# add - existing non-read-only profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command add" -f -a "(__qwdtt_user_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command add" -f -l device-id -d "Device ID"

# log/lg - profile names from existing log files and flags
complete -c qwdtt -n "__qwdtt_seen_command log lg" -f -a "(__qwdtt_log_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command log lg" -f -s n -l n -d "Number of lines to show"
complete -c qwdtt -n "__qwdtt_seen_command log lg" -f -s f -l follow -d "Follow log in real-time"

# list/ls - optional group filter and flags
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -a "(qwdtt __complete_groups 2>/dev/null)" -d "Group"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -l en -d "Show only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -l enabled -d "Show only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -l dis -d "Show only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -l disabled -d "Show only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -l ro -d "Show only read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -s sub -l sub -d "Show only profiles managed by any subscription"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -f -l active -d "Show only running profiles"

# test - profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command test; and not __qwdtt_last_is_group_flag" -f -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s ro -l ro -d "Test only read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s en -l en -d "Test only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s enabled -l enabled -d "Test only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s dis -l dis -d "Test only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s disabled -l disabled -d "Test only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -l group -r -a "(__qwdtt_groups)" -d "Test all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command test" -s sub -l sub -d "Test all profiles managed by any subscription"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s timeout -l timeout -d "Connection timeout in seconds"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s mode -l mode -d "Connection mode" -a "tun socks raw"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s socks-port -l socks-port -d "SOCKS5 port"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s socks-user -l socks-user -d "SOCKS5 username (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s socks-password -l socks-password -d "SOCKS5 password (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s transport -l transport -d "Transport to TURN relay" -a "udp tcp"
complete -c qwdtt -n "__qwdtt_seen_command test" -f -s delay -l delay -d "Pause between profiles in seconds"

# subscription (sub) subcommands
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -eq 2" -f -a "add remove rm show sh move mv update upd" -d "subscription subcommand"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = add" -f -a "(__fish_complete_path)" -d "Subscription URL"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3" -a "(__qwdtt_subscriptions)" -d "Subscription name"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub" -s y -l y -d "Skip confirmation prompt"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = add" -f -l yes -d "Skip confirmation prompt"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 3; and test (commandline -opc)[3] = move; and test (count (commandline -opc)) -le 3" -a "(__qwdtt_subscriptions)" -d "Subscription name"
complete -c qwdtt -n "__qwdtt_seen_command subscription sub; and test (count (commandline -opc)) -ge 4; and test (commandline -opc)[3] = move; and test (commandline -opc)[4] = mv" -a "(__qwdtt_subscriptions)" -d "Subscription name"
