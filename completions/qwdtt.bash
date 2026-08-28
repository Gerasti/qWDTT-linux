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
        local commands="connect disconnect switch debug add edit remove move list show share enable disable import bl device-id regenerate-id log test subscription version help"
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
        sub) cmd="subscription" ;;
    esac

    # Complete profile names for commands that need them
    case "$cmd" in
        connect)
            if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                local running=$(qwdtt __complete_running 2>/dev/null)
                local _filtered=""
                if [[ -n "$profiles" ]]; then
                    for p in $profiles; do
                        grep -qxF "$p" <<< "$running" || _filtered="$_filtered $p"
                    done
                fi
                COMPREPLY=( $(compgen -W "$_filtered" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-workers --workers -mtu --mtu -hashes --hashes -dns --dns -captcha --captcha -timeout --timeout -auto-switch --auto-switch -mode --mode -socks-port --socks-port -socks-user --socks-user -socks-password --socks-password -pub --pub --public -raw-port --raw-port -transport --transport -toggle --toggle -log --log -black-list --black-list -bl --bl -black-list-file --black-list-file -bl-file --bl-file" -- "$cur") )
             elif [[ $prev == "-captcha" || $prev == "--captcha" ]]; then
                COMPREPLY=( $(compgen -W "auto rjs" -- "$cur") )
             elif [[ $prev == "-mode" || $prev == "--mode" ]]; then
                 COMPREPLY=( $(compgen -W "tun socks raw" -- "$cur") )
             elif [[ $prev == "-transport" || $prev == "--transport" ]]; then
                COMPREPLY=( $(compgen -W "udp tcp" -- "$cur") )
             elif [[ $prev == "-bl" || $prev == "--bl" || $prev == "-black-list" || $prev == "--black-list" ]]; then
                 COMPREPLY=( $(compgen -f -- "$cur") )
             elif [[ $prev == "-bl-file" || $prev == "--bl-file" || $prev == "-black-list-file" || $prev == "--black-list-file" ]]; then
                COMPREPLY=( $(compgen -f -- "$cur") )
            fi
            ;;
        show)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-group --group -sub --sub" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        share)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-qwdtt -q --qwdtt --q -group --group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        edit)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-peer --peer -password --password -hashes --hashes -device-id --device-id -listen --listen -priority --priority -workers --workers -groups --groups -group --group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        remove)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-group --group -y --y -yes --yes" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            fi
            ;;
        enable)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-ro --ro -sub --sub -group --group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local disabled_profiles=$(qwdtt __complete_disabled 2>/dev/null)
                COMPREPLY=( $(compgen -W "$disabled_profiles" -- "$cur") )
            fi
            ;;
        disable)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-ro --ro -sub --sub -group --group" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local enabled_profiles=$(qwdtt __complete_enabled 2>/dev/null)
                COMPREPLY=( $(compgen -W "$enabled_profiles" -- "$cur") )
            fi
            ;;
        test)
            if [[ $prev == "-group" || $prev == "--group" ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local all_profiles=$(qwdtt __complete_all 2>/dev/null)
                COMPREPLY=( $(compgen -W "$all_profiles" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-ro --ro -en --en -enabled --enabled -dis --dis -disabled --disabled -group --group -sub --sub -timeout --timeout -mode --mode -socks-port --socks-port -socks-user --socks-user -socks-password --socks-password -pub --pub --public -transport --transport -delay --delay" -- "$cur") )
             elif [[ $prev == "-mode" || $prev == "--mode" ]]; then
                COMPREPLY=( $(compgen -W "tun socks raw" -- "$cur") )
            fi
            ;;
        disconnect)
            if [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "--all -all --y -y --yes -yes" -- "$cur") )
            elif [[ $COMP_CWORD -eq 2 ]]; then
                local running_profiles=$(qwdtt __complete_running 2>/dev/null)
                COMPREPLY=( $(compgen -W "$running_profiles" -- "$cur") )
            fi
            ;;
        add)
            if [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-device-id --device-id" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local user_profiles=$(qwdtt __complete_user 2>/dev/null)
                COMPREPLY=( $(compgen -W "$user_profiles" -- "$cur") )
            fi
            ;;
        debug|deb)
            # These commands don't take arguments
            ;;
        list)
            if [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local group_names=$(qwdtt __complete_groups 2>/dev/null)
                COMPREPLY=( $(compgen -W "$group_names" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-en --en -enabled --enabled -dis --dis -disabled --disabled -ro --ro -sub --sub -active --active -A --A -no-ip --no-ip" -- "$cur") )
            fi
            ;;
        log|lg)
            if [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-n --n -f --f -follow --follow" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                local log_profiles=$(qwdtt __complete_logs 2>/dev/null)
                COMPREPLY=( $(compgen -W "$log_profiles" -- "$cur") )
            fi
            ;;
        regenerate-id|version|help)
            # These commands don't take arguments
            ;;
        import)
            if [[ $prev == "-dry-run" || $prev == "--dry-run" ]]; then
                COMPREPLY=( $(compgen -f -- "$cur") )
            elif [[ $cur == -* ]]; then
                COMPREPLY=( $(compgen -W "-dry-run --dry-run" -- "$cur") )
            elif [[ $COMP_CWORD -ge 2 && $cur != -* ]]; then
                COMPREPLY=( $(compgen -f -- "$cur") )
            fi
            ;;
         bl)
             if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                  COMPREPLY=( $(compgen -W "init add list ls remove rm find fd load unload" -- "$cur") )
              elif [[ $prev == "-file" || $prev == "--file" || $prev == "-f" || $prev == "--f" ]]; then
                  local IFS=$'\n'
                  COMPREPLY=( $(compgen -f -- "$cur") $(compgen -d -- "$cur") )
             elif [[ $prev == "-profile" || $prev == "--profile" || $prev == "-p" || $prev == "--p" ]]; then
                local IFS=$'\n'
                COMPREPLY=( $(compgen -W "$(qwdtt __complete_running 2>/dev/null)" -- "$cur") )
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-file --file -f --f -y --y -profile --profile -p --p -r --r -reload --reload" -- "$cur") )
                if [[ " ${COMPREPLY[*]} " == *" $cur "* ]]; then
                    COMPREPLY=( "$cur" )
                fi
              elif [[ $COMP_CWORD -ge 3 && $cur != -* ]]; then
                  if [[ "${COMP_WORDS[2]}" == "init" || "${COMP_WORDS[2]}" == "load" ]]; then
                      local IFS=$'\n'
                      COMPREPLY=( $(compgen -f -- "$cur") $(compgen -d -- "$cur") )
                  fi
              fi
            ;;
        subscription|sub)
            if [[ $COMP_CWORD -eq 2 && $cur != -* ]]; then
                COMPREPLY=( $(compgen -W "add remove rm show sh move mv update upd" -- "$cur") )
            elif [[ $COMP_CWORD -eq 3 && $cur != -* ]]; then
                case "${COMP_WORDS[2]}" in
                    add)
                        COMPREPLY=( $(compgen -f -- "$cur") )
                        ;;
                    remove|rm|show|sh|update|upd|move|mv)
                        COMPREPLY=( $(compgen -W "$(qwdtt __complete_subscriptions 2>/dev/null)" -- "$cur") )
                        ;;
                esac
            elif [[ $cur == -* ]]; then
                 COMPREPLY=( $(compgen -W "-y --y -yes --yes" -- "$cur") )
            fi
            ;;
    esac

    return 0
}

complete -F _qwdtt_completions qwdtt
