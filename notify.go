package main

import (
	"fmt"
	"sync"
	"time"
	"github.com/godbus/dbus/v5"
)

const appName = "qWDTT"
var notifyMu sync.Mutex

func notifyDBus(summary, body, icon string) {
	notifyMu.Lock()
	defer notifyMu.Unlock()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.Notifications",
		dbus.ObjectPath("/org/freedesktop/Notifications"))
	err = obj.Call("org.freedesktop.Notifications.Notify", 0,
		appName,
		uint32(0),
		icon,
		summary,
		body,
		[]string{},
		map[string]dbus.Variant{},
		int32(5000),
	).Err
	if err != nil {
		fmt.Printf("[WARNING] Не удалось отправить уведомление: %v\n", err)
	}
}

// modeLabel converts an internal mode string into a human-readable label
// for display in notifications.
func modeLabel(mode string) string {
	switch mode {
	case "socks":
		return "SOCKS5"
	case "raw":
		return "RAW"
	default:
		return "TUN"
	}
}

func notifyConnected(profileName, mode string, workers int32) {
	go notifyDBus(fmt.Sprintf("%s: Подключено [%s]", appName, modeLabel(mode)),
		fmt.Sprintf("Подключение активно: %s\nАктивных воркеров: %d", profileName, workers),
		"network-transmit-receive")
}

func notifyDisconnected(profileName, mode string) {
	go notifyDBus(fmt.Sprintf("%s: Отключено [%s]", appName, modeLabel(mode)),
		fmt.Sprintf("Соединение разорвано: %s", profileName),
		"network-offline")
}

func notifyDisconnectedSync(profileName, mode string) {
	notifyDBus(fmt.Sprintf("%s: Отключено [%s]", appName, modeLabel(mode)),
		fmt.Sprintf("Соединение разорвано: %s", profileName),
		"network-offline")
}

func notifyError(profileName, errMsg string) {
	go notifyDBus(appName+": Ошибка подключения",
		fmt.Sprintf("%s: %s", profileName, errMsg),
		"dialog-error")
}

func notifyVKAuth(profileName string) {
	go notifyDBus(appName+": Аутентификация пройдена",
		fmt.Sprintf("VK auth успешна: %s", profileName),
		"changes-allow-symbolic")
}

var lastWorkerNotif time.Time
var workerNotifMu sync.Mutex

func notifyWorkers(profileName string, workers int32) {
	workerNotifMu.Lock()
	defer workerNotifMu.Unlock()
	if time.Since(lastWorkerNotif) < 60*time.Second {
		return
	}
	lastWorkerNotif = time.Now()
	sendNotification(fmt.Sprintf("qwdtt: %s", profileName),
		fmt.Sprintf("Активных воркеров: %d", workers),
		"network-transmit-receive")
}

func sendNotification(summary, body, icon string) {
	go notifyDBus(summary, body, icon)
}
