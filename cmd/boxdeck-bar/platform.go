package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func openURL(target string) error {
	return openTarget(target)
}

func openPath(target string) error {
	return openTarget(target)
}

func openTarget(target string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Run()
	default:
		return exec.Command("xdg-open", target).Run()
	}
}

func sendNotification(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("osascript", "-e", fmt.Sprintf("display notification %q with title %q", body, title)).Run()
	case "windows":
		title = strings.ReplaceAll(title, "'", "''")
		body = strings.ReplaceAll(body, "'", "''")
		command := "$xml=[Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime]::new();$xml.LoadXml('<toast><visual><binding template=\"ToastGeneric\"><text>'+[Security.SecurityElement]::Escape('" + title + "')+'</text><text>'+[Security.SecurityElement]::Escape('" + body + "')+'</text></binding></visual></toast>');[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('boxdeck-bar').Show([Windows.UI.Notifications.ToastNotification]::new($xml))"
		return exec.Command("powershell", "-NoProfile", "-Command", command).Run()
	default:
		return exec.Command("notify-send", title, body).Run()
	}
}
