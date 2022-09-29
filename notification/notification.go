// Package notification provides an interface for sending desktop notifications
// and handling signals (events).
//
// For more details see the specification:
// https://specifications.freedesktop.org/notification-spec/notification-spec-latest.html
package notification

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus"
)

const (
	PackageVersion  = "0.2.1"
	ExpiresNever    = time.Duration(0)        // notification never expires
	ExpiresDefault  = time.Duration(-1000000) // depends on the server's settings
	busName         = "org.freedesktop.Notifications"
	objPath         = "/org/freedesktop/Notifications"
	busInterface    = "org.freedesktop.Notifications"
	sigBufferSize   = 10
	ReasonExpired   = 1 // the notification expired
	ReasonDismissed = 2 // the notification was dismissed by the user
	ReasonClosed    = 3 // the notification was closed by a call to CloseNotification
	ReasonUndefined = 4 // undefined/reserved reasons
)

type Urgency byte

const (
	UrgencyLow      Urgency = 0
	UrgencyNormal   Urgency = 1
	UrgencyCritical Urgency = 2
)

var (
	AppName       string
	AppIcon       string
	busConn       *dbus.Conn
	busObj        dbus.BusObject
	notifications map[uint32]*Notification
)

// SendNotification sends a simple notification.
func SendNotification(summary, body, appName, appIcon string, urgency Urgency, timeout time.Duration) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("notification: Failed to connect to session bus: %w", err)
	}
	obj := conn.Object(busName, objPath)
	hints := make(map[string]dbus.Variant, 1)
	hints["urgency"] = dbus.MakeVariant(urgency)
	var icon string
	if appIcon == "" {
		icon = ""
	} else {
		icon, _ = filepath.Abs(appIcon)
	}
	call := obj.Call(busInterface+".Notify", 0, appName, uint32(0), icon, summary, body,
		make([]string, 0), hints, int32(timeout.Seconds()*1000))
	if call.Err != nil {
		return fmt.Errorf("notification: %w", call.Err)
	}
	return nil
}

// Init connects to the session bus, sets the appName and appIcon and
// starts an event loop.
func Init(appName, appIcon string) error {
	AppName = appName
	AppIcon = appIcon
	var err error
	busConn, err = dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("notification: Failed to connect to session bus: %w", err)
	}
	notifications = make(map[uint32]*Notification)
	busObj = busConn.Object(busName, objPath)
	err = addMatch("NotificationClosed")
	if err != nil {
		return fmt.Errorf("notification: %w", err)
	}
	err = addMatch("ActionInvoked")
	if err != nil {
		return fmt.Errorf("notification: %w", err)
	}
	c := make(chan *dbus.Signal, sigBufferSize)
	busConn.Signal(c)
	go func() {
		for {
			sig := <-c
			if strings.HasSuffix(sig.Name, ".NotificationClosed") {
				notificationClosedHandler(sig.Body[0].(uint32), sig.Body[1].(uint32))
			} else if strings.HasSuffix(sig.Name, ".ActionInvoked") {
				actionInvokedHandler(sig.Body[0].(uint32), sig.Body[1].(string))
			}
		}
	}()
	return nil
}

func addMatch(member string) error {
	call := busConn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		fmt.Sprintf("type='signal',path='%s',member='%s'", objPath, member))
	return call.Err
}

func actionInvokedHandler(id uint32, key string) {
	noti, ok := notifications[id]
	if ok {
		action, ok := noti.actions[key]
		if ok {
			go action.handler()
		}
	}
}

func notificationClosedHandler(id, reason uint32) {
	noti, ok := notifications[id]
	if ok {
		delete(notifications, id)
		if noti.closedHandler != nil {
			go noti.closedHandler(reason)
		}
	}
}

// GetCapabilities calls org.freedesktop.Notifications.GetCapabilities.
func GetCapabilities() (result []string, err error) {
	err = busObj.Call(busInterface+".GetCapabilities", 0).Store(&result)
	if err != nil {
		err = fmt.Errorf("notification: %w", err)
	}
	return
}

// ServerInfo represents server information.
type ServerInfo struct {
	Name        string
	Vendor      string
	Version     string
	SpecVersion string
}

// GetServerInformation calls org.freedesktop.Notifications.GetServerInformation.
func GetServerInformation() (*ServerInfo, error) {
	call := busObj.Call(busInterface+".GetServerInformation", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("notification: %w", call.Err)
	}
	serverInfo := ServerInfo{call.Body[0].(string), call.Body[1].(string),
		call.Body[2].(string), call.Body[3].(string)}
	return &serverInfo, nil
}

// Notify sends a notification.
func Notify(noti *Notification) error {
	var icon string
	if busObj == nil {
		return fmt.Errorf("notification: dbus object is empty")
	}
	if noti.icon == "" {
		icon = AppIcon
	} else {
		icon = noti.icon
	}
	if icon != "" {
		icon, _ = filepath.Abs(icon)
	}
	noti.hints["urgency"] = dbus.MakeVariant(noti.urgency)
	err := busObj.Call(busInterface+".Notify", 0, AppName, noti.id, icon, noti.summary, noti.body,
		noti.actionlist(), noti.hints, int32(noti.timeout.Seconds()*1000)).Store(&noti.id)
	if err != nil {
		err = fmt.Errorf("notification: %w", err)
	} else {
		notifications[noti.id] = noti
	}
	return err
}

// CloseNotification closes a notification.
func CloseNotification(noti *Notification) error {
	return busObj.Call(busInterface+".CloseNotification", 0, noti.id).Err
}

type action struct {
	name    string
	handler func()
}

// Notification represents a desktop notification.
// A notification can be modified and updated/shown again on the screen with Notify().
type Notification struct {
	id            uint32
	icon          string
	summary       string
	body          string
	urgency       Urgency
	timeout       time.Duration
	actions       map[string]action
	hints         map[string]dbus.Variant
	closedHandler func(uint32)
}

// New creates a new Notification.
// The urgency will be set to UrgencyNormal and the timeout to ExpiresDefault.
func New(summary, body string) *Notification {
	noti := Notification{}
	noti.summary = summary
	noti.body = body
	noti.urgency = UrgencyNormal
	noti.timeout = ExpiresDefault
	noti.hints = make(map[string]dbus.Variant, 1)
	return &noti
}

// SetIcon sets the notification's icon.
// If icon is an empty string AppIcon will be used.
func (noti *Notification) SetIcon(icon string) {
	noti.icon = icon
}

// SetSummary sets the notification's summary.
// This is a single line overview of the notification.
func (noti *Notification) SetSummary(summary string) {
	noti.summary = summary
}

// SetBody sets the notification's body.
// This is a multi-line body of text.
func (noti *Notification) SetBody(body string) {
	noti.body = body
}

// SetUrgency sets the notification's urgency level.
// This is one of the Urgency* constants.
func (noti *Notification) SetUrgency(urgency Urgency) {
	noti.urgency = urgency
}

// SetTimeout sets the expiration timeout.
// This is the duration after which the notification should be closed
// or one of the constants ExpiresNever or ExpiresDefault.
func (noti *Notification) SetTimeout(timeout time.Duration) {
	noti.timeout = timeout
}

// AddHint adds a hint to the notification.
// A hint with the key "urgency" will be ignored; use SetUrgency().
// See the specification for more details.
func (noti *Notification) AddHint(key string, value interface{}) {
	if value == nil {
		delete(noti.hints, key)
	} else {
		noti.hints[key] = dbus.MakeVariant(value)
	}
}

// SetClosedHandler sets a function to handle the
// org.freedesktop.Notifications.NotificationClosed signal.
// This function gets one of the Reason* constants as its arguement.
// Setting handler to nil will remove the function.
func (noti *Notification) SetClosedHandler(handler func(uint32)) {
	noti.closedHandler = handler
}

// AddActionHandler adds an action and a function to handle an
// org.freedesktop.Notifications.ActionInvoked signal with the specified key.
// Setting handler to nil will remove the function.
func (noti *Notification) AddActionHandler(key, name string, handler func()) {
	if handler == nil {
		delete(noti.actions, key)
	} else {
		if noti.actions == nil {
			noti.actions = make(map[string]action, 1)
		}
		noti.actions[key] = action{name, handler}
	}
}

func (noti *Notification) actionlist() []string {
	list := make([]string, 0, 2*len(noti.actions))
	for key, action := range noti.actions {
		list = append(list, key, action.name)
	}
	return list
}
