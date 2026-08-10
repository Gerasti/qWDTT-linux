#!/usr/bin/env bash
# Bash completion for qwdtt

_qwdtt_completions() {
    local cur prev opts profiles
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Get profile names from config directory
    local config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/qwdtt/profiles"
    if [[ -d "$config_dir" ]]; then
        profiles=$(qwdtt __complete_enabled 2>/dev/null)
    fi

    # Complete main command - show only primary commands, no aliases
    if [[ $COMP_CWORD -eq 1 ]]; then
        local commands="connect disconnect debug add edit remove move list show share enable disable import device-id regenerate-id log test version help"
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
        discon) cmd="disconnect" ;;
        sh) cmd="show" ;;
        ls) cmd="list" ;;
        rm) cmd="remove" ;;
        mv) cmd="move" ;;
        en) cmd="enable" ;;
        dis) cmd="disable" ;;
        id) cmd="device-id" ;;
        lg) cmd="log" ;;
        deb) cmd="debug" ;;
    esac

    # Complete profile names for commands that need them
    case "$cmd" in
        connect)
            if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                COMPREPLY=( $(compgen -W "$profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-workers -mtu -hashes -dns -captcha -timeout -auto-switch -mode -socks-port -toggle -black-list -bl -black-list-file -bl-file" -- "$cur") )
            elif [[ $prev == "-captcha" ]]; then
                COMPREPLY=( $(compgen -W "auto rjs" -- "$cur") )
            elif [[ $prev == "-mode" ]]; then
                COMPREPLY=( $(compgen -W "tun socks" -- "$cur") )
            elif [[ $prev == "-bl" || $prev == "-black-list" ]]; then
                COMPREPLY=( $(compgen -f -- "$cur") )
            elif [[ $prev == "-bl-file" || $prev == "-black-list-file" ]]; then
                COMPREPLY=( $(compgen -f -- "$cur") )
            fi
            ;;
        show|share)
            if [[ $prev == "-group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        remove)
            if [[ $prev == "-group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-group -y -yes" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        enable)
            if [[ $prev == "-group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local disabled_profiles=$(qwdtt __complete_disabled 2>/dev/null)
                COMPREPLY=( $(compgen -W "$disabled_profiles" -- "$cur") )
            fi
            ;;
        disable)
            if [[ $prev == "-group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                COMPREPLY=( $(compgen -W "$profiles" -- "$cur") )
            fi
            ;;
        edit)
            if [[ $prev == "-group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-peer -password -hashes -device-id -listen -priority -groups -group" -- "$cur") )
            fi
            ;;
        move)
            if [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                 local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
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
        log)
            if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                local log_profiles=$(qwdtt __complete_logs 2>/dev/null)
                COMPREPLY=( $(compgen -W "$log_profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-n -follow -f" -- "$cur") )
            fi
            ;;
        test)
            if [[ $prev == "-group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-ro -en -enabled -dis -disabled -group -timeout -mode -socks-port" -- "$cur") )
            elif [[ $prev == "-mode" ]]; then
                COMPREPLY=( $(compgen -W "tun socks" -- "$cur") )
            fi
            ;;
        disconnect|debug|deb)
            # These commands don't take arguments
            ;;
        list)
            if [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-en -enabled -dis -disabled -ro" -- "$cur") )
            fi
            ;;
        regenerate-id|version|help)
            # These commands don't take arguments
            ;;
    esac

    return 0
}

complete -F _qwdtt_completions qwdtt
