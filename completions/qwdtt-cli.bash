#!/usr/bin/env bash
# Bash completion for qwdtt-cli

_qwdtt_cli_completions() {
    local cur prev opts profiles
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Get profile names from config directory
    local config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/qwdtt/profiles"
    if [[ -d "$config_dir" ]]; then
        profiles=$(qwdtt-cli __complete_enabled 2>/dev/null)
    fi

    # Complete main command - show only primary commands, no aliases
    if [[ $COMP_CWORD -eq 1 ]]; then
        local commands="connect add edit remove list show enable disable device-id regenerate-id version help"
        # Manually filter to avoid substring matching of aliases
        local matches=()
        for word in $commands; do
            if [[ $word == "$cur"* ]]; then
                matches+=("$word")
            fi
        done
        COMPREPLY=("${matches[@]}")
        return 0
    fi

    local cmd="${COMP_WORDS[1]}"

    # Normalize aliases to full commands for consistent handling
    case "$cmd" in
        con) cmd="connect" ;;
        sh) cmd="show" ;;
        ls) cmd="list" ;;
        rm) cmd="remove" ;;
        id) cmd="device-id" ;;
    esac

    # Complete profile names for commands that need them
    case "$cmd" in
        connect)
            if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                COMPREPLY=( $(compgen -W "$profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-workers -mtu -hashes -timeout -auto-switch" -- "$cur") )
            fi
            ;;
        show|remove)
            if [[ $COMP_CWORD -eq 2 ]]; then
                local all_profiles=$(qwdtt-cli __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        enable)
            if [[ $COMP_CWORD -eq 2 ]]; then
                local disabled_profiles=$(qwdtt-cli __complete_disabled 2>/dev/null)
                COMPREPLY=( $(compgen -W "$disabled_profiles" -- "$cur") )
            fi
            ;;
        disable)
            if [[ $COMP_CWORD -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "$profiles" -- "$cur") )
            fi
            ;;
        edit)
            if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt-cli __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-peer -password -hashes -device-id -listen -priority" -- "$cur") )
            fi
            ;;
        add)
            if [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-device-id" -- "$cur") )
            fi
            ;;
        device-id)
            # No completion for device-id argument
            ;;
        list|regenerate-id|version|help)
            # These commands don't take arguments
            ;;
    esac

    return 0
}

complete -F _qwdtt_cli_completions qwdtt-cli
