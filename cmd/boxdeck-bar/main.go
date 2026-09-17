package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/wicolian/boxdeck/internal/barclient"
)

type barApp struct {
	mu          sync.Mutex
	cfg         barclient.Config
	configPath  string
	model       barclient.MenuModel
	previous    map[string]barclient.BoxSnapshot
	notified    map[string]time.Time
	seenRefresh bool
	stop        chan struct{}
}

func newBarApp(config barclient.Config, path string) *barApp {
	return &barApp{cfg: config, configPath: path, previous: map[string]barclient.BoxSnapshot{}, notified: map[string]time.Time{}, stop: make(chan struct{})}
}

func (a *barApp) refresh() {
	boxes := barclient.FetchFleet(context.Background(), a.cfg)
	a.mu.Lock()
	retainFailureStart(boxes, a.previous)
	a.notifyTransitions(boxes)
	for _, box := range boxes {
		a.previous[box.URL] = box
	}
	a.seenRefresh = true
	model := barclient.BuildMenu(boxes)
	a.mu.Unlock()
	if _, err := exec.LookPath("boxdeck"); err == nil {
		usageCtx, usageCancel := context.WithTimeout(context.Background(), 30*time.Second)
		usage, usageErr := barclient.ReadLocalUsage(usageCtx)
		usageCancel()
		if usageErr == nil {
			model.LocalUsage = &barclient.LocalUsage{Title: barclient.LocalUsageTitle(usage)}
		}
	}
	a.mu.Lock()
	a.model = model
	a.mu.Unlock()
	a.render()
}

func retainFailureStart(boxes []barclient.BoxSnapshot, previous map[string]barclient.BoxSnapshot) {
	for i := range boxes {
		if boxes[i].OK || boxes[i].URL == "" {
			continue
		}
		if old, ok := previous[boxes[i].URL]; ok && !old.OK && old.Since != "" {
			boxes[i].Since = old.Since
		}
	}
}

func (a *barApp) notifyTransitions(boxes []barclient.BoxSnapshot) {
	if !a.cfg.Notify || !a.seenRefresh {
		return
	}
	now := time.Now()
	for _, box := range boxes {
		old, existed := a.previous[box.URL]
		if !existed {
			continue
		}
		if !old.OK && box.OK {
			continue
		}
		if old.OK && !box.OK {
			a.notifyOnce(box.URL+":down", now, box.Name, "Boxdeck box is unreachable")
		}
		if !hasNeedsYou(old) && hasNeedsYou(box) {
			a.notifyOnce(box.URL+":needs", now, box.Name, "An agent needs your attention")
		}
		oldAlerts := map[string]bool{}
		for _, alert := range old.Alerts {
			oldAlerts[alert.ID] = true
		}
		for _, alert := range box.Alerts {
			if !oldAlerts[alert.ID] {
				a.notifyOnce(box.URL+":alert:"+alert.ID, now, box.Name, alert.Title)
			}
		}
	}
}

func hasNeedsYou(box barclient.BoxSnapshot) bool {
	for _, agent := range box.State.Agents {
		status := agent.Status
		if status == "needs_you" || status == "needs-you" || status == "waiting" || status == "blocked" {
			return true
		}
	}
	return false
}

func (a *barApp) notifyOnce(key string, now time.Time, title, body string) {
	if sent, ok := a.notified[key]; ok && now.Sub(sent) < 5*time.Minute {
		return
	}
	a.notified[key] = now
	go func() { _ = sendNotification(title, body) }()
}

func (a *barApp) render() {
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	templateIcon, _ := barclient.IconPNG(barclient.IconTemplate)
	regularIcon, _ := barclient.IconPNG(model.IconState)
	if model.IconState == barclient.IconHealthy {
		systray.SetTemplateIcon(templateIcon, regularIcon)
	} else {
		systray.SetIcon(regularIcon)
	}
	systray.SetTooltip(model.Tooltip)
	systray.ResetMenu()
	if len(model.Needs) > 0 {
		section := systray.AddMenuItem("Needs you", "Open alerts")
		_ = section
		for _, alert := range model.Needs {
			item := systray.AddMenuItem(alert.Box+": "+alert.Title, "Open alert")
			go menuOpen(item, alert.URL)
			ack := systray.AddMenuItem("  Ack", "Acknowledge alert")
			go menuPost(ack, alert.AckURL, alert.Token, "{}")
			if alert.ActionLabel != "" {
				action := systray.AddMenuItem("  "+alert.ActionLabel, "Run alert action")
				go menuPost(action, alert.ActionURL, alert.Token, alert.ActionBody)
			}
		}
		systray.AddSeparator()
	}
	for _, box := range model.Boxes {
		name := systray.AddMenuItem(box.Title, "Open this boxdeck")
		go menuOpen(name, box.URL)
		for _, line := range box.Lines {
			item := systray.AddMenuItem(line.Title, line.Title)
			if line.Action != barclient.ActionNone {
				go menuAction(item, line.Action, line.URL, a)
			}
		}
		systray.AddSeparator()
	}
	if model.AddBox {
		item := systray.AddMenuItem("Add a box", "Create bar.json and open it")
		go menuAction(item, barclient.ActionAdd, "", a)
	}
	if model.LocalUsage != nil {
		systray.AddMenuItem(model.LocalUsage.Title, "Today's local provider usage")
	}
	refresh := systray.AddMenuItem("Refresh", "Refresh all boxes")
	go menuAction(refresh, barclient.ActionRefresh, "", a)
	settings := systray.AddMenuItem("Settings", "Open bar.json")
	go menuAction(settings, barclient.ActionSettings, "", a)
	quit := systray.AddMenuItem("Quit", "Quit boxdeck-bar")
	go menuAction(quit, barclient.ActionQuit, "", a)
}

func menuOpen(item *systray.MenuItem, target string) {
	if target == "" {
		return
	}
	<-item.ClickedCh
	_ = openURL(target)
}

func menuAction(item *systray.MenuItem, action barclient.LineAction, target string, app *barApp) {
	<-item.ClickedCh
	switch action {
	case barclient.ActionOpen, barclient.ActionAgents, barclient.ActionUsage:
		_ = openURL(target)
	case barclient.ActionAdd:
		if err := barclient.WriteTemplate(app.configPath); err == nil {
			_ = openPath(app.configPath)
		}
	case barclient.ActionRefresh:
		go app.refresh()
	case barclient.ActionSettings:
		_ = openPath(app.configPath)
	case barclient.ActionQuit:
		close(app.stop)
		systray.Quit()
	}
}

func menuPost(item *systray.MenuItem, target, token, body string) {
	<-item.ClickedCh
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewBufferString(body))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
}

func alertActionBody(value string) string {
	var body any
	if json.Unmarshal([]byte(value), &body) != nil {
		return "{}"
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func printModel(configPath string) error {
	config, err := barclient.LoadConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		model := barclient.BuildMenu(nil)
		fmt.Println(barclient.RenderText(model))
		return nil
	}
	if err != nil {
		return err
	}
	boxes := barclient.FetchFleet(context.Background(), config)
	model := barclient.BuildMenu(boxes)
	if _, err := exec.LookPath("boxdeck"); err == nil {
		usageCtx, usageCancel := context.WithTimeout(context.Background(), 30*time.Second)
		usage, usageErr := barclient.ReadLocalUsage(usageCtx)
		usageCancel()
		if usageErr == nil {
			model.LocalUsage = &barclient.LocalUsage{Title: barclient.LocalUsageTitle(usage)}
		}
	}
	fmt.Println(barclient.RenderText(model))
	return nil
}

func runBar(configPath string) error {
	config, err := barclient.LoadConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		config = barclient.DefaultConfig()
	} else if err != nil {
		return err
	}
	app := newBarApp(config, configPath)
	systray.Run(func() {
		systray.SetTitle("boxdeck")
		app.model = barclient.BuildMenu(nil)
		app.render()
		go func() {
			app.refresh()
			ticker := time.NewTicker(time.Duration(app.cfg.RefreshSec) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					go app.refresh()
				case <-app.stop:
					return
				}
			}
		}()
	}, func() {})
	return nil
}

func main() {
	configOverride := flag.String("config", "", "path to bar.json")
	printOnly := flag.Bool("print", false, "print the current menu model and exit")
	iconOutput := flag.String("icon", "", "write the generated tray icon PNG and exit")
	flag.Parse()
	if *iconOutput != "" {
		data, err := barclient.IconPNG(barclient.IconTemplate)
		if err == nil {
			err = os.WriteFile(*iconOutput, data, 0600)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "boxdeck-bar:", err)
			os.Exit(1)
		}
		return
	}
	configPath := *configOverride
	if configPath == "" {
		var err error
		configPath, err = barclient.DefaultConfigPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, "boxdeck-bar:", err)
			os.Exit(1)
		}
	}
	var err error
	if *printOnly {
		err = printModel(filepath.Clean(configPath))
	} else {
		err = runBar(filepath.Clean(configPath))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "boxdeck-bar:", err)
		os.Exit(1)
	}
}
