package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *app) tokenAuthed(r *http.Request) bool {
	if len(a.cfg.Tokens) == 0 && !strings.HasPrefix(strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer "), "act.") {
		return false
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if ok && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.ContainsAny(token, " \t\r\n") {
		if strings.HasPrefix(token, "act.") {
			return a.actionTokenAllows(token, r)
		}
		return tokenMatches(a.cfg.Tokens, token)
	}
	if r.URL.Path == "/cdp" || strings.HasPrefix(r.URL.Path, "/cdp/") {
		queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
		if queryToken != "" && !strings.ContainsAny(queryToken, " \t\r\n") {
			return tokenMatches(a.cfg.Tokens, queryToken)
		}
	}
	return false
}

func tokenMatches(configured []string, token string) bool {
	want := sha256.Sum256([]byte(token))
	valid := false
	for _, value := range configured {
		got := sha256.Sum256([]byte(value))
		if subtle.ConstantTimeCompare(got[:], want[:]) == 1 {
			valid = true
		}
	}
	return valid
}

func tokenLabel(cfg config, token string) string {
	if cfg.TokenLabels == nil {
		return ""
	}
	return cfg.TokenLabels[token]
}

func addLabeledToken(cfg *config, token, label string) {
	cfg.Tokens = append(cfg.Tokens, token)
	if strings.TrimSpace(label) == "" {
		return
	}
	if cfg.TokenLabels == nil {
		cfg.TokenLabels = map[string]string{}
	}
	cfg.TokenLabels[token] = label
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "bd_" + hex.EncodeToString(b), nil
}

func saveConfig(c config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err = os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), ".boxdeck-config-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func tokenCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: boxdeck token [new|list|revoke PREFIX]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(configPath(home), home)
	if err != nil {
		return err
	}
	return tokenCommandWithConfig(&cfg, args)
}

func tokenCommandWithConfig(cfg *config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: boxdeck token [new|list|revoke PREFIX]")
	}
	switch args[0] {
	case "new":
		if len(args) != 1 {
			return fmt.Errorf("usage: boxdeck token new")
		}
		token, err := randomToken()
		if err != nil {
			return err
		}
		cfg.Tokens = append(cfg.Tokens, token)
		if err := saveConfig(*cfg); err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: boxdeck token list")
		}
		if len(cfg.Tokens) == 0 {
			fmt.Println("No tokens")
			return nil
		}
		for i, token := range cfg.Tokens {
			prefix := token
			if len(prefix) > 12 {
				prefix = prefix[:12]
			}
			label := tokenLabel(*cfg, token)
			if label != "" {
				fmt.Printf("%d %s... (%d characters) [%s]\n", i+1, prefix, len(token), label)
			} else {
				fmt.Printf("%d %s... (%d characters)\n", i+1, prefix, len(token))
			}
		}
		return nil
	case "revoke":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: boxdeck token revoke PREFIX")
		}
		prefix := args[1]
		found := -1
		for i, token := range cfg.Tokens {
			if strings.HasPrefix(token, prefix) {
				if found >= 0 {
					return fmt.Errorf("token prefix is ambiguous")
				}
				found = i
			}
		}
		if found < 0 {
			return fmt.Errorf("no token starts with %q", prefix)
		}
		revoked := cfg.Tokens[found]
		cfg.Tokens = append(cfg.Tokens[:found], cfg.Tokens[found+1:]...)
		if cfg.TokenLabels != nil {
			delete(cfg.TokenLabels, revoked)
		}
		if err := saveConfig(*cfg); err != nil {
			return err
		}
		fmt.Println("Token revoked")
		return nil
	default:
		return fmt.Errorf("usage: boxdeck token [new|list|revoke PREFIX]")
	}
}
