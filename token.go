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

// Read-only tokens (bdr_ prefix, kept in readOnlyTokens) can only read a short allowlist of
// status endpoints. They live in their own config list, so an older binary that does not know
// about them rejects them instead of treating them as full tokens.
const readOnlyTokenPrefix = "bdr_"

// readOnlyPaths is the complete list of paths a read-only token may GET or HEAD.
var readOnlyPaths = map[string]bool{
	"/api/boxes":     true,
	"/api/agents":    true,
	"/api/usage":     true,
	"/api/usage/all": true,
	"/api/health":    true,
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if ok && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.ContainsAny(token, " \t\r\n") {
		return token, true
	}
	return "", false
}

// readOnlyBearer reports whether the request carries a configured read-only bearer token.
func (a *app) readOnlyBearer(r *http.Request) bool {
	token, ok := bearerToken(r)
	return ok && len(a.cfg.ReadOnlyTokens) > 0 && tokenMatches(a.cfg.ReadOnlyTokens, token)
}

// readOnlyAllows is the fail-closed gate for a read-only token: GET or HEAD, no upgrade,
// and an exact path from readOnlyPaths.
func readOnlyAllows(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("Upgrade") != "" {
		return false
	}
	return readOnlyPaths[r.URL.Path]
}

func (a *app) tokenAuthed(r *http.Request) bool {
	if a.readOnlyBearer(r) {
		return readOnlyAllows(r)
	}
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
	setTokenLabel(cfg, token, label)
}

func randomToken() (string, error) {
	return randomPrefixedToken("bd_")
}

func randomPrefixedToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

func setTokenLabel(cfg *config, token, label string) {
	if strings.TrimSpace(label) == "" {
		return
	}
	if cfg.TokenLabels == nil {
		cfg.TokenLabels = map[string]string{}
	}
	cfg.TokenLabels[token] = label
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

const tokenUsage = "usage: boxdeck token [new [--read-only] [LABEL]|list|revoke PREFIX]"

func tokenCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(tokenUsage)
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
		return fmt.Errorf(tokenUsage)
	}
	switch args[0] {
	case "new":
		readOnly, label := false, ""
		for _, arg := range args[1:] {
			switch {
			case arg == "--read-only":
				readOnly = true
			case strings.HasPrefix(arg, "-") || label != "":
				return fmt.Errorf("usage: boxdeck token new [--read-only] [LABEL]")
			default:
				label = arg
			}
		}
		prefix := "bd_"
		if readOnly {
			prefix = readOnlyTokenPrefix
		}
		token, err := randomPrefixedToken(prefix)
		if err != nil {
			return err
		}
		if readOnly {
			cfg.ReadOnlyTokens = append(cfg.ReadOnlyTokens, token)
		} else {
			cfg.Tokens = append(cfg.Tokens, token)
		}
		setTokenLabel(cfg, token, label)
		if err := saveConfig(*cfg); err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: boxdeck token list")
		}
		all := append(append([]string{}, cfg.Tokens...), cfg.ReadOnlyTokens...)
		if len(all) == 0 {
			fmt.Println("No tokens")
			return nil
		}
		for i, token := range all {
			prefix := token
			if len(prefix) > 12 {
				prefix = prefix[:12]
			}
			kind := ""
			if i >= len(cfg.Tokens) {
				kind = " read-only"
			}
			label := tokenLabel(*cfg, token)
			if label != "" {
				fmt.Printf("%d %s... (%d characters)%s [%s]\n", i+1, prefix, len(token), kind, label)
			} else {
				fmt.Printf("%d %s... (%d characters)%s\n", i+1, prefix, len(token), kind)
			}
		}
		return nil
	case "revoke":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: boxdeck token revoke PREFIX")
		}
		prefix := args[1]
		found, readOnly := -1, false
		for i, token := range cfg.Tokens {
			if strings.HasPrefix(token, prefix) {
				if found >= 0 {
					return fmt.Errorf("token prefix is ambiguous")
				}
				found = i
			}
		}
		for i, token := range cfg.ReadOnlyTokens {
			if strings.HasPrefix(token, prefix) {
				if found >= 0 {
					return fmt.Errorf("token prefix is ambiguous")
				}
				found, readOnly = i, true
			}
		}
		if found < 0 {
			return fmt.Errorf("no token starts with %q", prefix)
		}
		var revoked string
		if readOnly {
			revoked = cfg.ReadOnlyTokens[found]
			cfg.ReadOnlyTokens = append(cfg.ReadOnlyTokens[:found], cfg.ReadOnlyTokens[found+1:]...)
		} else {
			revoked = cfg.Tokens[found]
			cfg.Tokens = append(cfg.Tokens[:found], cfg.Tokens[found+1:]...)
		}
		if cfg.TokenLabels != nil {
			delete(cfg.TokenLabels, revoked)
		}
		if err := saveConfig(*cfg); err != nil {
			return err
		}
		fmt.Println("Token revoked")
		return nil
	default:
		return fmt.Errorf(tokenUsage)
	}
}
