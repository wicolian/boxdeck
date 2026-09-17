package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type ctlArgs struct {
	To      string
	Token   string
	Box     string
	Table   bool
	Command string
	Args    []string
}

func parseCtlArgs(argv []string) (ctlArgs, error) {
	var parsed ctlArgs
	var commandArgs []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--table":
			parsed.Table = true
		case arg == "--json":
			parsed.Table = false
		case arg == "--to", arg == "--token", arg == "--box":
			if i+1 >= len(argv) || argv[i+1] == "" {
				return ctlArgs{}, fmt.Errorf("%s needs a value", arg)
			}
			i++
			switch arg {
			case "--to":
				parsed.To = argv[i]
			case "--token":
				parsed.Token = argv[i]
			case "--box":
				parsed.Box = argv[i]
			}
		case strings.HasPrefix(arg, "--to="):
			parsed.To = strings.TrimPrefix(arg, "--to=")
		case strings.HasPrefix(arg, "--token="):
			parsed.Token = strings.TrimPrefix(arg, "--token=")
		case strings.HasPrefix(arg, "--box="):
			parsed.Box = strings.TrimPrefix(arg, "--box=")
		case strings.HasPrefix(arg, "-"):
			return ctlArgs{}, fmt.Errorf("unknown ctl option %q", arg)
		case parsed.Command == "":
			parsed.Command = arg
		default:
			commandArgs = append(commandArgs, arg)
		}
	}
	if parsed.Command == "" {
		return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl [--to URL] [--token TOKEN] [--box NAME] [--table] COMMAND")
	}
	switch parsed.Command {
	case "prompt":
		if len(commandArgs) < 2 {
			return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl prompt PANE TEXT")
		}
		parsed.Args = []string{commandArgs[0], strings.Join(commandArgs[1:], " ")}
	case "run":
		if len(commandArgs) == 0 {
			return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl run CMD")
		}
		parsed.Args = []string{strings.Join(commandArgs, " ")}
	case "focus", "kill", "files":
		if len(commandArgs) != 1 {
			return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl %s VALUE", parsed.Command)
		}
		parsed.Args = commandArgs
	case "state", "ports", "procs", "agents", "tmux", "usage":
		if len(commandArgs) != 0 {
			return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl %s", parsed.Command)
		}
	case "browser":
		if len(commandArgs) == 0 {
			return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl browser [start|stop|pages|shot PAGE OUT.png]")
		}
		switch commandArgs[0] {
		case "start", "stop", "pages":
			if len(commandArgs) != 1 {
				return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl browser %s", commandArgs[0])
			}
		case "shot":
			if len(commandArgs) != 3 {
				return ctlArgs{}, fmt.Errorf("usage: boxdeck ctl browser shot PAGE OUT.png")
			}
		default:
			return ctlArgs{}, fmt.Errorf("unknown browser command %q", commandArgs[0])
		}
		parsed.Args = commandArgs
	default:
		return ctlArgs{}, fmt.Errorf("unknown ctl command %q", parsed.Command)
	}
	return parsed, nil
}

type ctlClient struct {
	base  string
	token string
}

func (c ctlClient) request(method, path string, body any) ([]byte, string, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.base, "/")+path, reader)
	if err != nil {
		return nil, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	client := &http.Client{Timeout: 65 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("server returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, response.Header.Get("Content-Type"), nil
}

func ctlCommand(argv []string) error {
	args, err := parseCtlArgs(argv)
	if err != nil {
		return err
	}
	if args.To == "" {
		args.To = os.Getenv("BOXDECK_TO")
	}
	if args.Token == "" {
		args.Token = os.Getenv("BOXDECK_TOKEN")
	}
	var cfg config
	if args.Box != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		cfg, err = loadConfig(configPath(home), home)
		if err != nil {
			return err
		}
		if args.Box == "local" || args.Box == cfg.Host {
			args.To = "http://" + cfg.Host + ":" + strconv.Itoa(int(cfg.Port))
			if args.Token == "" && len(cfg.Tokens) > 0 {
				args.Token = cfg.Tokens[0]
			}
		} else {
			found := false
			for _, box := range cfg.Boxes {
				if box.Name == args.Box {
					args.To, args.Token, found = box.URL, box.Token, true
					break
				}
			}
			if !found {
				return fmt.Errorf("box %q is not in the local config", args.Box)
			}
		}
	}
	if args.To == "" {
		return fmt.Errorf("set --to URL or BOXDECK_TO")
	}
	args.To, err = normalizeBoxURL(args.To)
	if err != nil {
		return err
	}
	if args.Token == "" {
		return fmt.Errorf("set --token TOKEN or BOXDECK_TOKEN")
	}
	client := ctlClient{base: args.To, token: args.Token}
	method, path, body, err := ctlRequest(args)
	if err != nil {
		return err
	}
	data, _, err := client.request(method, path, body)
	if err != nil {
		return err
	}
	if args.Command == "browser" && args.Args[0] == "shot" {
		var shot struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(data, &shot); err != nil || shot.Data == "" {
			return fmt.Errorf("browser returned no screenshot")
		}
		image, err := base64.StdEncoding.DecodeString(shot.Data)
		if err != nil {
			return fmt.Errorf("invalid screenshot data")
		}
		if err := os.WriteFile(args.Args[2], image, 0600); err != nil {
			return err
		}
		fmt.Println("saved", args.Args[2])
		return nil
	}
	if args.Table {
		return printCtlTable(args.Command, data)
	}
	_, err = os.Stdout.Write(data)
	return err
}

func ctlRequest(args ctlArgs) (string, string, any, error) {
	switch args.Command {
	case "state":
		return http.MethodGet, "/api/state", nil, nil
	case "ports":
		return http.MethodGet, "/api/ports", nil, nil
	case "procs":
		return http.MethodGet, "/api/procs?sort=cpu&n=20", nil, nil
	case "agents":
		return http.MethodGet, "/api/agents", nil, nil
	case "tmux":
		return http.MethodGet, "/api/tmux", nil, nil
	case "usage":
		return http.MethodGet, "/api/usage", nil, nil
	case "focus":
		return http.MethodPost, "/api/herdr/focus", object{"pane_id": args.Args[0]}, nil
	case "prompt":
		return http.MethodPost, "/api/herdr/prompt", object{"pane_id": args.Args[0], "text": args.Args[1]}, nil
	case "kill":
		pid, err := strconv.Atoi(args.Args[0])
		if err != nil || pid < 1 {
			return "", "", nil, fmt.Errorf("PID must be a positive integer")
		}
		return http.MethodPost, "/api/proc/kill", object{"pid": pid, "signal": 15}, nil
	case "files":
		return http.MethodGet, "/api/files?path=" + url.QueryEscape(args.Args[0]), nil, nil
	case "run":
		return http.MethodPost, "/api/run", object{"cmd": args.Args[0]}, nil
	case "browser":
		switch args.Args[0] {
		case "start":
			return http.MethodPost, "/api/browser/start", object{"headless": true}, nil
		case "stop":
			return http.MethodPost, "/api/browser/stop", object{}, nil
		case "pages":
			return http.MethodGet, "/api/browser", nil, nil
		case "shot":
			return http.MethodGet, "/api/browser/shot?page=" + url.QueryEscape(args.Args[1]), nil, nil
		default:
			return "", "", nil, fmt.Errorf("unknown browser command %q", args.Args[0])
		}
	default:
		return "", "", nil, fmt.Errorf("unknown ctl command %q", args.Command)
	}
}

// cell renders a JSON value for a table: missing or null values print as blank, never <nil>.
func cell(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func printCtlTable(command string, data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		_, err = os.Stdout.Write(data)
		return err
	}
	if command == "ports" {
		fmt.Println("PORT\tPROC\tPID\tURL")
		for _, row := range arrayObjects(value) {
			fmt.Printf("%v\t%v\t%v\t%v\n", cell(row["port"]), cell(row["proc"]), cell(row["pid"]), cell(row["url"]))
		}
		return nil
	}
	if command == "procs" {
		fmt.Println("PID\tCPU\tRSS\tARGS")
		for _, row := range arrayObjects(value) {
			fmt.Printf("%v\t%v\t%v\t%v\n", cell(row["pid"]), cell(row["cpu"]), cell(row["rss"]), cell(row["args"]))
		}
		return nil
	}
	if command == "agents" {
		fmt.Println("PID\tKIND\tSTATUS\tPANE\tCWD")
		for _, row := range arrayObjects(value) {
			fmt.Printf("%v\t%v\t%v\t%v\t%v\n", cell(row["pid"]), cell(row["kind"]), cell(row["agent_status"]), cell(row["pane_id"]), cell(row["cwd"]))
		}
		return nil
	}
	if command == "usage" {
		if root, ok := value.(map[string]any); ok {
			fmt.Println("PROVIDER\tTODAY TOKENS\tTODAY COST\tSESSIONS")
			providers, _ := root["providers"].(map[string]any)
			for _, provider := range []string{"claude", "codex"} {
				row, _ := providers[provider].(map[string]any)
				today, _ := row["today"].(map[string]any)
				tokens, _ := today["tokens"].(map[string]any)
				var total float64
				for _, key := range []string{"in", "cachedIn", "cacheWrite", "out"} {
					if n, ok := tokens[key].(float64); ok {
						total += n
					}
				}
				cost, _ := today["costUsd"].(float64)
				fmt.Printf("%s\t%.0f\t$%.4f\t%v\n", provider, total, cost, cell(row["sessions"]))
			}
			fmt.Println("estimate at list price")
			return nil
		}
	}
	if command == "tmux" || command == "boxes" {
		for _, row := range arrayObjects(value) {
			fmt.Printf("%v\n", row)
		}
		return nil
	}
	if command == "run" {
		if row, ok := value.(map[string]any); ok {
			if stdout, ok := row["stdout"].(string); ok {
				fmt.Print(stdout)
				if stdout != "" && !strings.HasSuffix(stdout, "\n") {
					fmt.Println()
				}
			}
			if stderr, ok := row["stderr"].(string); ok && stderr != "" {
				fmt.Fprint(os.Stderr, stderr)
			}
			fmt.Printf("exit %v\n", cell(row["exitCode"]))
			return nil
		}
	}
	if command == "browser" {
		if row, ok := value.(map[string]any); ok {
			for _, key := range []string{"running", "pid", "port", "memory", "error"} {
				if v, exists := row[key]; exists {
					fmt.Printf("%s\t%v\n", key, cell(v))
				}
			}
			if pages, ok := row["pages"].([]any); ok {
				for _, page := range pages {
					if item, ok := page.(map[string]any); ok {
						fmt.Printf("page\t%s\t%s\t%s\n", cell(item["id"]), cell(item["title"]), cell(item["url"]))
					}
				}
			}
			return nil
		}
	}
	if row, ok := value.(map[string]any); ok {
		for _, key := range []string{"title", "host", "ok", "focused", "pane_id", "stdout", "exitCode"} {
			if v, exists := row[key]; exists {
				fmt.Printf("%s\t%v\n", key, v)
			}
		}
		return nil
	}
	fmt.Println(value)
	return nil
}

func arrayObjects(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			result = append(result, row)
		}
	}
	return result
}
