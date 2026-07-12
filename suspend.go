package main

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

func monitorSuspendResume(notifyCh chan<- bool, stopCh <-chan struct{}) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		fmt.Println("[DEBUG] D-Bus недоступен, мониторинг suspend/resume отключен")
		return
	}
	defer conn.Close()

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/login1"),
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		fmt.Printf("[DEBUG] Не удалось подписаться на события suspend/resume: %v\n", err)
		return
	}

	signals := make(chan *dbus.Signal, 10)
	conn.Signal(signals)

	fmt.Println("[DEBUG] Мониторинг suspend/resume через D-Bus активен")

	for {
		select {
		case <-stopCh:
			return
		case sig := <-signals:
			if sig.Name == "org.freedesktop.login1.Manager.PrepareForSleep" && len(sig.Body) > 0 {
				if sleeping, ok := sig.Body[0].(bool); ok {
					if sleeping {
						fmt.Println("\n[DEBUG] Система уходит в suspend...")
					} else {
						fmt.Println("\n[DEBUG] Resume обнаружен через D-Bus")
						select {
						case notifyCh <- true:
						default:
						}
						return
					}
				}
			}
		}
	}
}
