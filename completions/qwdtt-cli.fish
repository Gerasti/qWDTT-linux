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
complete -c qwdtt-cli -n __fish_use_subcommand -a connect -d "Подключиться к VPN"
complete -c qwdtt-cli -n __fish_use_subcommand -a disconnect -d "Отключиться от VPN"
complete -c qwdtt-cli -n __fish_use_subcommand -a debug -d "Отладочная информация"
complete -c qwdtt-cli -n __fish_use_subcommand -a add -d "Добавить профиль"
complete -c qwdtt-cli -n __fish_use_subcommand -a edit -d "Редактировать профиль"
complete -c qwdtt-cli -n __fish_use_subcommand -a remove -d "Удалить профиль"
complete -c qwdtt-cli -n __fish_use_subcommand -a list -d "Список профилей"
complete -c qwdtt-cli -n __fish_use_subcommand -a show -d "Показать профиль"
complete -c qwdtt-cli -n __fish_use_subcommand -a enable -d "Включить профиль"
complete -c qwdtt-cli -n __fish_use_subcommand -a disable -d "Отключить профиль"
complete -c qwdtt-cli -n __fish_use_subcommand -a device-id -d "Показать/установить Device ID"
complete -c qwdtt-cli -n __fish_use_subcommand -a regenerate-id -d "Перегенерировать Device ID"
complete -c qwdtt-cli -n __fish_use_subcommand -a version -d "Версия"
complete -c qwdtt-cli -n __fish_use_subcommand -a help -d "Помощь"

# connect/con - profile names and flags
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -a "(__qwdtt_profiles)" -d "Профиль"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l workers -d "Количество воркеров"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l mtu -d "MTU туннеля"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l hashes -d "VK-хеши через запятую"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l timeout -d "Таймаут подключения в секундах"
complete -c qwdtt-cli -n "__qwdtt_seen_command connect con" -l auto-switch -d "Автопереключение при неудаче"

# show, remove - all profile names
complete -c qwdtt-cli -n "__qwdtt_seen_command show sh remove rm" -a "(__qwdtt_all_profiles)" -d "Профиль"

# enable/en - only disabled profiles
complete -c qwdtt-cli -n "__qwdtt_seen_command enable en" -a "(__qwdtt_disabled_profiles)" -d "Профиль"

# disable/dis - only enabled profiles
complete -c qwdtt-cli -n "__qwdtt_seen_command disable dis" -a "(__qwdtt_profiles)" -d "Профиль"

# edit - all profile names and flags
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -a "(__qwdtt_all_profiles)" -d "Профиль"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l peer -d "Адрес сервера (IP:PORT)"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l password -d "Пароль"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l hashes -d "VK-хеши через запятую"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l device-id -d "Device ID"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l listen -d "Локальный адрес"
complete -c qwdtt-cli -n "__qwdtt_seen_command edit" -l priority -d "Приоритет профиля"

# add - flags only
complete -c qwdtt-cli -n "__qwdtt_seen_command add" -l device-id -d "Device ID"
