package notification

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/godbus/dbus"
)

const (
	Version        = "0.0.1"
	ExpiresNever   = time.Duration(0)
	ExpiresDefault = time.Duration(-1000000)
	busName        = "org.freedesktop.Notifications"
	objPath        = "/org/freedesktop/Notifications"
	busInterface   = "org.freedesktop.Notifications"
)

type Urgency byte

const (
	Low      Urgency = 0
	Normal   Urgency = 1
	Critical Urgency = 2
)

func SendNotification(summary, body, app_name, app_icon string, urgency Urgency, timeout time.Duration) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("notification: Failed to connect to session bus: %v", err)
	}
	defer conn.Close()
	obj := conn.Object(busName, objPath)
	hints := make(map[string]dbus.Variant, 1)
	hints["urgency"] = dbus.MakeVariant(urgency)
	var icon string
	if app_icon == "" {
		icon = ""
	} else {
		icon, _ = filepath.Abs(app_icon)
	}
	call := obj.Call(busInterface+".Notify", 0, app_name, uint32(0), icon, summary, body,
		make([]string, 0), hints, int32(timeout.Seconds()*1000))
	if call.Err != nil {
		return fmt.Errorf("notification: %v", call.Err)
	}
	return nil
}
