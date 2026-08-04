package main

import (
	"fmt"
	"sync"

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

func notifyConnected(profileName string) {
	go notifyDBus(appName+": Подключено", fmt.Sprintf("Подключение активно: %s", profileName))
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
