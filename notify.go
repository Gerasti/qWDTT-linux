package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const appName = "qWDTT"

var notifyMu sync.Mutex

func notifyDBus(summary, body string) {
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
		"",
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

func notifyConnected(profileName string, workers int32) {
	go notifyDBus(appName+": Подключено", fmt.Sprintf("Подключение активно: %s\nАктивных воркеров: %d", profileName, workers))
}

func notifyDisconnected(profileName string) {
	go notifyDBus(appName+": Отключено", fmt.Sprintf("Соединение разорвано: %s", profileName))
}

func notifyDisconnectedSync(profileName string) {
	notifyDBus(appName+": Отключено", fmt.Sprintf("Соединение разорвано: %s", profileName))
}

func notifyError(profileName, errMsg string) {
	go notifyDBus(appName+": Ошибка подключения", fmt.Sprintf("%s: %s", profileName, errMsg))
}

func notifyVKAuth(profileName string) {
	go notifyDBus(appName+": Аутентификация пройдена", fmt.Sprintf("VK auth успешна: %s", profileName))
}

var lastWorkerNotif time.Time
var workerNotifMu sync.Mutex

func notifyWorkers(profileName string, workers int32) {
	workerNotifMu.Lock()
	defer workerNotifMu.Unlock()
	// Throttle: don't send worker notifications more than once per 60 seconds
	if time.Since(lastWorkerNotif) < 60*time.Second {
		return
	}
	lastWorkerNotif = time.Now()
	sendNotification(fmt.Sprintf("qwdtt: %s", profileName), fmt.Sprintf("Активных воркеров: %d", workers))
}

func sendNotification(summary, body string) {
	go notifyDBus(summary, body)
}
