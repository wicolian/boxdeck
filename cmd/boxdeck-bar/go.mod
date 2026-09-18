module github.com/wicolian/boxdeck/cmd/boxdeck-bar

go 1.27.1

require (
	fyne.io/systray v1.12.2
	github.com/wicolian/boxdeck v0.0.0
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
)

replace github.com/wicolian/boxdeck => ../..
