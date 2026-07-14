# Fish completion for qwdtt-cli

# Helper function to get enabled profile names
function __qwdtt_profiles
    qwdtt-cli __complete_enabled 2>/dev/null
end

# Helper function to get all profile names
function __qwdtt_all_profiles
    qwdtt-cli __complete_all 2>/dev/null
end

# Helper function to get disabled profile names
function __qwdtt_disabled_profiles
    qwdtt-cli __complete_disabled 2>/dev/null
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
complete -c qwdtt-cli -f
complete -c qwdtt-cli -n __fish_use_subcommand -a connect -d "Connect to VPN"
complete -c qwdtt-cli -n __fish_use_subcommand -a disconnect -d "Disconnect from VPN"
complete -c qwdtt-cli -n __fish_use_subcommand -a debug -d "Show debug information"
complete -c qwdtt-cli -n __fish_use_subcommand -a add -d "Add profile"
complete -c qwdtt-cli -n __fish_use_subcommand -a edit -d "Edit profile"
complete -c qwdtt-cli -n __fish_use_subcommand -a remove -d "Remove profile"
complete -c qwdtt-cli -n __fish_use_subcommand -a list -d "List profiles"
complete -c qwdtt-cli -n __fish_use_subcommand -a show -d "Show profile"
complete -c qwdtt-cli -n __fish_use_subcommand -a enable -d "Enable profile"
complete -c qwdtt-cli -n __fish_use_subcommand -a disable -d "Disable profile"
complete -c qwdtt-cli -n __fish_use_subcommand -a device-id -d "Show/set Device ID"
complete -c qwdtt-cli -n __fish_use_subcommand -a regenerate-id -d "Regenerate Device ID"
complete -c qwdtt-cli -n __fish_use_subcommand -a version -d "Show version"
complete -c qwdtt-cli -n __fish_use_subcommand -a help -d "Show help"

# connect/con - profile names and flags
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -a "(__qwdtt_profiles)" -d "Profile"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l workers -d "Number of workers"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l mtu -d "Tunnel MTU"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l dns -d "DNS resolver"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l timeout -d "Connection timeout (seconds)"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l auto-switch -d "Auto-switch on failure"

# show, remove - all profile names
complete -c qwdtt-cli -n "__qwdtt_seen_command show sh remove rm" -a "(__qwdtt_all_profiles)" -d "Profile"

# enable/en - only disabled profiles
complete -c qwdtt-cli -n "__qwdtt_seen_command enable en" -a "(__qwdtt_disabled_profiles)" -d "Profile"

# disable/dis - only enabled profiles
complete -c qwdtt-cli -n "__qwdtt_seen_command disable dis" -a "(__qwdtt_profiles)" -d "Profile"

# edit - all profile names and flags
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -a "(__qwdtt_all_profiles)" -d "Profile"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l peer -d "Server address (IP:PORT)"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l password -d "Password"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l hashes -d "VK hashes (comma-separated)"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l device-id -d "Device ID"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l listen -d "Local address"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l priority -d "Profile priority"

# add - flags only
complete -c qwdtt-cli -n "__qwdtt_seen_command add" -l device-id -d "Device ID"
