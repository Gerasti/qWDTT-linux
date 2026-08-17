package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type WdttLink struct {
	IP       string
	DTLSPort string
	Password string
	Hashes   []string
	Name     string
	Workers  int    // количество воркеров (по умолчанию 9)
	Port     string // локальный порт прослушивания (по умолчанию "9000")
}

const (
	defaultWorkers    = 9
	defaultListenPort = "9000"
)

// validateWorkers проверяет, что количество воркеров допустимо:
// должно быть больше 0 и кратно defaultWorkers (9).
func validateWorkers(n int) error {
	if n <= 0 {
		return fmt.Errorf("не может быть 0 или отрицательным")
	}
	if n%defaultWorkers != 0 {
		return fmt.Errorf("должно быть кратно %d", defaultWorkers)
	}
	return nil
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
		Workers:  defaultWorkers,
		Port:     defaultListenPort,
	}, nil
}

// parseQwdttConfigURL разбирает ссылку вида:
//
//	qwdtt://config?name=Дом&peer=1.2.3.4:56000&hashes=хеш1,хеш2&workers=18&port=9000&pass=секрет
//
// Поддерживаются URL-encoded значения. Все параметры доступны через query string.
func parseQwdttConfigURL(raw string) (*WdttLink, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("не удалось распарсить URL: %w", err)
	}

	q := u.Query()

	name := q.Get("name")
	if strings.TrimSpace(name) == "" {
		name = "Server"
	}

	peer := strings.TrimSpace(q.Get("peer"))
	if peer == "" {
		return nil, fmt.Errorf("параметр peer обязателен")
	}

	ip := ""
	dtlsPort := ""
	if idx := strings.LastIndex(peer, ":"); idx != -1 {
		ip = peer[:idx]
		dtlsPort = peer[idx+1:]
	} else {
		ip = peer
	}

	password := q.Get("pass")
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("параметр pass обязателен")
	}

	var hashes []string
	if hashesStr := q.Get("hashes"); hashesStr != "" {
		for _, h := range strings.Split(hashesStr, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				hashes = append(hashes, h)
			}
		}
	}

	workers := defaultWorkers
	if w := q.Get("workers"); w != "" {
		n, err := strconv.Atoi(w)
		if err != nil {
			return nil, fmt.Errorf("параметр workers должен быть числом, получено: %s", w)
		}
		if err := validateWorkers(n); err != nil {
			return nil, fmt.Errorf("параметр workers: %w", err)
		}
		workers = n
	}

	port := strings.TrimSpace(q.Get("port"))
	if port == "" {
		port = defaultListenPort
	}

	if ip == "" || dtlsPort == "" {
		return nil, fmt.Errorf("не указаны обязательные поля (ip или port в peer)")
	}

	return &WdttLink{
		IP:       ip,
		DTLSPort: dtlsPort,
		Password: password,
		Hashes:   hashes,
		Name:     name,
		Workers:  workers,
		Port:     port,
	}, nil
}

// parseLink определяет схему ссылки и делегирует парсинг
// соответствующей функции. Поддерживает wdtt:// и qwdtt://config?...
func parseLink(raw string) (*WdttLink, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "wdtt://") {
		return parseWdttURL(trimmed)
	}
	if strings.HasPrefix(trimmed, "qwdtt://") {
		return parseQwdttConfigURL(trimmed)
	}
	return nil, fmt.Errorf("неизвестный формат ссылки: поддерживаются wdtt:// и qwdtt://config")
}

// buildQwdttConfigURL обратное кодирование WdttLink в формат qwdtt://config?...
// Все значения URL-кодируются через url.Values.Encode().
func buildQwdttConfigURL(link *WdttLink) string {
	q := url.Values{}
	q.Set("name", link.Name)
	q.Set("peer", fmt.Sprintf("%s:%s", link.IP, link.DTLSPort))
	q.Set("pass", link.Password)
	if len(link.Hashes) > 0 {
		q.Set("hashes", strings.Join(link.Hashes, ","))
	}
	if link.Workers > 0 {
		q.Set("workers", strconv.Itoa(link.Workers))
	}
	if link.Port != "" {
		q.Set("port", link.Port)
	}
	return "qwdtt://config?" + q.Encode()
}
