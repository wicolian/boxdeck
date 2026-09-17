package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func serviceUnit(executable, path, home string) string {
	quote := func(s string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, "\n", `\n`).Replace(s) + `"`
	}
	return "[Unit]\nDescription=boxdeck: one-page cockpit for this box\n\n[Service]\nExecStart=" + quote(executable) + " serve\nEnvironment=" + quote("BOXDECK_CONFIG="+path) + "\nEnvironment=" + quote("PATH="+filepath.Join(home, ".local/bin")+":"+filepath.Join(home, "go/bin")+":/usr/local/bin:/usr/bin:/bin") + "\nRestart=always\nRestartSec=3\nNice=10\n\n[Install]\nWantedBy=default.target\n"
}
func install() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := configPath(home)
	if _, err = os.Stat(path); os.IsNotExist(err) {
		terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("open a terminal and run boxdeck install to choose a password")
		}
		defer terminal.Close()
		reader := bufio.NewReader(terminal)
		ask := func(label, def string) (string, error) {
			fmt.Fprint(terminal, label)
			s, err := reader.ReadString('\n')
			s = strings.TrimSpace(s)
			if s == "" {
				s = def
			}
			return s, err
		}
		user, err := ask("Login user [admin]: ", "admin")
		if err != nil {
			return err
		}
		// stty is only used by the optional interactive installer, never by serve.
		mode := exec.Command("stty", "-g")
		mode.Stdin = terminal
		original, modeErr := mode.Output()
		if modeErr != nil {
			return fmt.Errorf("cannot hide password input: %w", modeErr)
		}
		echo := exec.Command("stty", "-echo")
		echo.Stdin = terminal
		if err = echo.Run(); err != nil {
			return err
		}
		password, readErr := ask("Password: ", "")
		restore := exec.Command("stty", strings.TrimSpace(string(original)))
		restore.Stdin = terminal
		restoreErr := restore.Run()
		fmt.Fprintln(terminal)
		if readErr != nil {
			return readErr
		}
		if restoreErr != nil {
			return restoreErr
		}
		if password == "" {
			return fmt.Errorf("choose a nonempty password and run boxdeck install again")
		}
		hostname, _ := os.Hostname()
		host, err := ask("Hostname your browser uses ["+hostname+"]: ", hostname)
		if err != nil {
			return err
		}
		host, err = normalizeHost(host)
		if err != nil {
			return err
		}
		b, err := json.MarshalIndent(object{"user": user, "password": password, "host": host, "port": 8100, "filesPort": 0, "quick": []any{}, "reportRoots": []string{"~/reports", "~/box"}, "known": object{}, "tokens": []string{}, "boxes": []any{}, "allowRun": false}, "", "  ")
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(append(b, '\n'))
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if err != nil {
		return err
	}
	cfg, err := loadConfig(path, home)
	if err != nil {
		return err
	}
	if _, err = loadSecret(filepath.Join(filepath.Dir(path), "secret")); err != nil {
		return err
	}
	if runtime.GOOS != "linux" {
		fmt.Println("Config ready. Run boxdeck serve; automatic service installation needs Linux systemd.")
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config/systemd/user")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "boxdeck.service"), []byte(serviceUnit(executable, path, home)), 0644); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "boxdeck.service"}, {"--user", "restart", "boxdeck.service"}} {
		cmd := exec.Command("systemctl", args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err = cmd.Run(); err != nil {
			return fmt.Errorf("systemctl failed: %w", err)
		}
	}
	user := os.Getenv("USER")
	if user == "" {
		return fmt.Errorf("set USER, then run loginctl enable-linger for your account")
	}
	linger := exec.Command("loginctl", "enable-linger", user)
	linger.Stdout, linger.Stderr = os.Stdout, os.Stderr
	if err = linger.Run(); err != nil {
		return fmt.Errorf("service started; enable linger with: loginctl enable-linger %s", user)
	}
	_, err = io.WriteString(os.Stdout, fmt.Sprintf("boxdeck is ready at http://%s:%d\n", cfg.Host, cfg.Port))
	return err
}
