package main

import (
	"fmt"
	"strings"
)

type WdttLink struct {
	IP       string
	DTLSPort string
	Password string
	Hashes   []string
	Name     string
}

func parseWdttURL(raw string) (*WdttLink, error) {
	stripped := strings.TrimPrefix(strings.TrimSpace(raw), "wdtt://")
	parts := strings.Split(stripped, ":")
	if len(parts) < 5 {
		return nil, fmt.Errorf("неверный формат URL (нужно минимум 5 полей)")
	}

	ip := parts[0]
	dtlsPort := parts[1]
	tail := strings.Join(parts[4:], ":")

	name := "Server"
	hashIdx := strings.LastIndex(tail, "#")
	passwordAndHashes := tail
	if hashIdx != -1 {
		candidate := strings.TrimSpace(tail[hashIdx+1:])
		if candidate != "" {
			name = candidate
		}
		passwordAndHashes = tail[:hashIdx]
	}

	colonIdx := strings.LastIndex(passwordAndHashes, ":")
	var password string
	var hashes []string
	if colonIdx != -1 {
		password = passwordAndHashes[:colonIdx]
		hashStr := passwordAndHashes[colonIdx+1:]
		for _, h := range strings.Split(hashStr, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				hashes = append(hashes, h)
			}
		}
	} else {
		password = passwordAndHashes
	}

	if ip == "" || dtlsPort == "" || password == "" {
		return nil, fmt.Errorf("не указаны обязательные поля")
	}

	return &WdttLink{
		IP:       ip,
		DTLSPort: dtlsPort,
		Password: password,
		Hashes:   hashes,
		Name:     name,
	}, nil
}
