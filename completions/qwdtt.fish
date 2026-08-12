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

# Helper function to get group names
function __qwdtt_groups
    qwdtt __complete_groups 2>/dev/null
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
complete -c qwdtt -n __fish_use_subcommand -a device-id -d "Show/set Device ID"
complete -c qwdtt -n __fish_use_subcommand -a regenerate-id -d "Regenerate Device ID"
complete -c qwdtt -n __fish_use_subcommand -a log -d "Show daemon log file"
complete -c qwdtt -n __fish_use_subcommand -a test -d "Test profile connectivity"
complete -c qwdtt -n __fish_use_subcommand -a version -d "Show version"
complete -c qwdtt -n __fish_use_subcommand -a help -d "Show help"

# connect/con - profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command connect con" -a "(__qwdtt_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l workers -d "Number of workers"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l mtu -d "Tunnel MTU"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l dns -d "DNS resolver"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l captcha -d "Captcha bypass mode" -a "auto rjs"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l timeout -d "Connection timeout (seconds)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l auto-switch -d "Auto-switch on failure"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l mode -d "Connection mode" -a "tun socks raw"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l socks-port -d "SOCKS5 port (only with -mode socks)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l raw-port -d "Raw mode server port (only with -mode raw)"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l toggle -d "Stop running profile, or start if not running"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -l black-list -a "bl" -d "These domains go direct"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -s bl -l bl -d "Alias for --black-list"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -r -l black-list-file -a "bl-file" -d "Read blacklist domains from JSON file"
complete -c qwdtt -n "__qwdtt_seen_command connect con" -r -s bl-file -l bl-file -d "Alias for --black-list-file"

# show, share - all profile names and -group flag
complete -c qwdtt -n "__qwdtt_seen_command show sh share" -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command show sh share" -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"

# remove/rm - all profile names, -group flag, confirmation bypass
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command remove rm" -s y -l y -l yes -d "Skip confirmation prompt"

# move/mv - all profile names for both arguments
complete -c qwdtt -n "__qwdtt_seen_command move mv" -a "(__qwdtt_all_profiles)" -d "Profile"

# enable/en - only disabled profiles and -group flag
complete -c qwdtt -n "__qwdtt_seen_command enable en" -a "(__qwdtt_disabled_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command enable en" -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"

# disable/dis - only enabled profiles and -group flag
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -a "(__qwdtt_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command disable dis" -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"

# edit - all profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command edit" -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l peer -d "Server address (IP:PORT)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l password -d "Password"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l device-id -d "Device ID"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l listen -d "Local address"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l priority -d "Profile priority"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l groups -d "Profile groups (comma-separated)"
complete -c qwdtt -n "__qwdtt_seen_command edit" -l group -r -a "(__qwdtt_groups)" -d "Operate on all profiles in this group"

# add - flags only
complete -c qwdtt -n "__qwdtt_seen_command add" -l device-id -d "Device ID"

# log/lg - profile names from existing log files and flags
complete -c qwdtt -n "__qwdtt_seen_command log lg" -a "(__qwdtt_log_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command log lg" -s n -l n -d "Number of lines to show"
complete -c qwdtt -n "__qwdtt_seen_command log lg" -s f -l follow -d "Follow log in real-time"

# list/ls - optional group filter and flags
complete -c qwdtt -n "__qwdtt_seen_command list ls" -a "(qwdtt __complete_groups 2>/dev/null)" -d "Group"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -l en -d "Show only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -l enabled -d "Show only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -l dis -d "Show only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -l disabled -d "Show only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command list ls" -l ro -d "Show only read-only profiles"

# test - profile names and flags
complete -c qwdtt -n "__qwdtt_seen_command test" -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt -n "__qwdtt_seen_command test" -s ro -l ro -d "Test only read-only profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -s en -l en -d "Test only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -s enabled -l enabled -d "Test only enabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -s dis -l dis -d "Test only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -s disabled -l disabled -d "Test only disabled profiles"
complete -c qwdtt -n "__qwdtt_seen_command test" -l group -r -a "(__qwdtt_groups)" -d "Test all profiles in this group"
complete -c qwdtt -n "__qwdtt_seen_command test" -s timeout -l timeout -d "Connection timeout in seconds"
complete -c qwdtt -n "__qwdtt_seen_command test" -s mode -l mode -d "Connection mode" -a "tun socks"
complete -c qwdtt -n "__qwdtt_seen_command test" -s socks-port -l socks-port -d "SOCKS5 port"
