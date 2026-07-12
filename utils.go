package main

import (
	"fmt"
	"os"
	"strings"
)

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func selectProfileInteractive() string {
	dir := profilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "Нет сохранённых профилей")
			fmt.Fprintln(os.Stderr, "Используйте: qwdtt-cli add <name> <wdtt://...>")
			return ""
		}
		fmt.Fprintf(os.Stderr, "Ошибка чтения профилей: %v\n", err)
		return ""
	}

	type profileEntry struct {
		name string
		peer string
	}

	var profiles []profileEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		prof, err := loadProfile(name)
		if err != nil {
			continue
		}
		if !prof.Enabled {
			continue
		}
		profiles = append(profiles, profileEntry{name: name, peer: prof.PeerAddr})
	}

	if len(profiles) == 0 {
		fmt.Fprintln(os.Stderr, "Нет включенных профилей")
		fmt.Fprintln(os.Stderr, "Используйте: qwdtt-cli add <name> <wdtt://...>")
		fmt.Fprintln(os.Stderr, "Или: qwdtt-cli enable <name>")
		return ""
	}

	fmt.Println("Выберите профиль:")
	for i, p := range profiles {
		fmt.Printf("  %d. %s (%s)\n", i+1, p.name, p.peer)
	}
	fmt.Print("> ")

	var choice int
	if _, err := fmt.Scanf("%d", &choice); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка ввода")
		return ""
	}

	if choice < 1 || choice > len(profiles) {
		fmt.Fprintln(os.Stderr, "Неверный выбор")
		return ""
	}

	return profiles[choice-1].name
}
