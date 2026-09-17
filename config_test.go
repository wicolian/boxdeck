package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPostel(t *testing.T) {
	for _, host := range []string{"box", " http://box/ ", "https://box/", "http://box:8100/"} {
		t.Run(host, func(t *testing.T) {
			home := t.TempDir()
			p := filepath.Join(home, "config.json")
			os.WriteFile(p, []byte(`{"password":"test","host":"`+host+`","port":"8103","filesPort":"0","ttydPort":"7682","terminal":false,"mirror":false,"hide":["22",53],"quick":[["App","3001"]]}`), 0600)
			c, err := loadConfig(p, home)
			if err != nil {
				t.Fatal(err)
			}
			if c.Host != "box" || c.Port != 8103 || c.FilesPort != 0 || c.TTYDPort != 7682 || c.Quick[0].Port != 3001 || c.Terminal {
				t.Fatal("normalization failed")
			}
		})
	}
	for _, host := range []string{"https://box/path", "https://user@box", "javascript:foo", "https://box/?x=1"} {
		if _, err := normalizeHost(host); err == nil {
			t.Errorf("accepted %s", host)
		}
	}
	if host, err := normalizeHost("http://[::1]:8103/"); err != nil || host != "::1" {
		t.Fatal("IPv6")
	}
}
func TestConfigEnvironment(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "config.json")
	os.WriteFile(p, []byte(`{"password":"test","host":"box","terminal":true,"filesPort":8090}`), 0600)
	t.Setenv("BOXDECK_PORT", "8103")
	t.Setenv("BOXDECK_FILES_PORT", "0")
	t.Setenv("BOXDECK_TERMINAL", "false")
	t.Setenv("BOXDECK_MIRROR", "0")
	c, err := loadConfig(p, home)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8103 || c.FilesPort != 0 || c.Terminal || c.Mirror != false {
		t.Fatal("env overrides")
	}
	t.Setenv("BOXDECK_PORT", "65536")
	if _, err = loadConfig(p, home); err == nil {
		t.Fatal("accepted impossible port")
	}
}
func TestServiceUnit(t *testing.T) {
	unit := serviceUnit("/home/test/a folder/boxdeck", "/home/test/config.json", "/home/test")
	for _, want := range []string{`ExecStart="/home/test/a folder/boxdeck" serve`, "Nice=10", "BOXDECK_CONFIG=", ".local/bin", "WantedBy=default.target"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %s", want)
		}
	}
}
