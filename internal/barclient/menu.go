package barclient

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func BuildMenu(boxes []BoxSnapshot) MenuModel {
	model := MenuModel{Boxes: make([]MenuBox, 0, len(boxes)), IconState: IconHealthy, Generated: time.Now()}
	for _, box := range boxes {
		menuBox := MenuBox{Title: box.Name, URL: box.URL}
		if box.Discovered || box.Tag == "tailnet" {
			menuBox.Title += " [tailnet]"
		}
		if !box.OK {
			menuBox.Lines = append(menuBox.Lines, MenuLine{Title: unreachableTitle(box.Since)})
			model.IconState = IconRust
			model.Boxes = append(model.Boxes, menuBox)
			continue
		}
		health := box.Health
		if health == (Health{}) {
			health = box.State.Health
		}
		portCount := box.PortCount
		if portCount == 0 {
			portCount = len(box.State.Ports)
		}
		menuBox.Lines = append(menuBox.Lines,
			MenuLine{Title: healthTitle(health)},
			MenuLine{Title: agentTitle(box)},
			MenuLine{Title: fmt.Sprintf("ports: %d open", portCount)},
		)
		for _, provider := range []string{"claude", "codex"} {
			usage := box.Usage.Providers[provider]
			line := quotaTitle(provider, usage)
			menuBox.Lines = append(menuBox.Lines, MenuLine{Title: line, Action: ActionUsage, URL: joinHash(box.URL, "/usage")})
			if quotaAttention(usage.Quota) && model.IconState != IconRust {
				model.IconState = IconAttention
			}
		}
		menuBox.Lines[1].Action = ActionAgents
		menuBox.Lines[1].URL = joinHash(box.URL, "/agents")
		if needsYou(box) && model.IconState != IconRust {
			model.IconState = IconAttention
		}
		model.Boxes = append(model.Boxes, menuBox)
	}
	if len(boxes) == 0 {
		model.AddBox = true
	}
	model.Tooltip = tooltip(boxes)
	return model
}

func healthTitle(health Health) string {
	return fmt.Sprintf("cpu %.0f%% mem %s/%s GB load %.1f", health.CPU, formatGB(health.MemUsed), formatGB(health.MemTotal), health.Load1)
}

func formatGB(value float64) string {
	if value <= 0 {
		return "0"
	}
	gb := value / (1024 * 1024 * 1024)
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", gb), "0"), ".")
}

func agentTitle(box BoxSnapshot) string {
	working, waiting := 0, 0
	for _, agent := range box.State.Agents {
		if isNeedsYou(agent.Status) {
			waiting++
		} else {
			working++
		}
	}
	if len(box.State.Agents) == 0 && box.AgentCount > 0 {
		working = box.AgentCount
	}
	return fmt.Sprintf("agents: %d working, %d needs you", working, waiting)
}

func isNeedsYou(status string) bool {
	status = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(status), "-", "_"))
	return status == "needs_you" || status == "waiting" || status == "blocked"
}

func needsYou(box BoxSnapshot) bool {
	for _, agent := range box.State.Agents {
		if isNeedsYou(agent.Status) {
			return true
		}
	}
	return false
}

func quotaTitle(provider string, usage UsageProvider) string {
	if usage.Quota == nil {
		return provider + " api key"
	}
	parts := []string{provider}
	if usage.Quota.FiveHour != nil {
		parts = append(parts, "5h", strconv.Itoa(int(usage.Quota.FiveHour.Pct))+"%")
	}
	if usage.Quota.SevenDay != nil {
		parts = append(parts, "7d", strconv.Itoa(int(usage.Quota.SevenDay.Pct))+"%")
	}
	if len(parts) == 1 {
		return provider + " quota unavailable"
	}
	return strings.Join(parts, " ")
}

func quotaAttention(quota *Quota) bool {
	if quota == nil {
		return false
	}
	return quota.FiveHour != nil && quota.FiveHour.Pct > 90 || quota.SevenDay != nil && quota.SevenDay.Pct > 90
}

func unreachableTitle(since string) string {
	if since == "" {
		return "unreachable"
	}
	parsed, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return "unreachable since " + since
	}
	return "unreachable since " + parsed.Local().Format("15:04")
}

func joinHash(base, route string) string {
	return strings.TrimRight(base, "/") + "/#" + route
}

func tooltip(boxes []BoxSnapshot) string {
	if len(boxes) == 0 {
		return "boxdeck: no boxes"
	}
	parts := make([]string, 0, len(boxes))
	for _, box := range boxes {
		if !box.OK {
			parts = append(parts, box.Name+" down")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d agents %d ports", box.Name, box.AgentCount, box.PortCount))
	}
	return strings.Join(parts, " | ")
}

func LocalUsageTitle(usage UsageResponse) string {
	parts := []string{}
	for _, provider := range []string{"claude", "codex"} {
		if row, ok := usage.Providers[provider]; ok {
			parts = append(parts, fmt.Sprintf("%s %d tokens $%.4f", provider, row.Today.Tokens.Total(), row.Today.CostUSD))
		}
	}
	if len(parts) == 0 {
		return "Local usage unavailable"
	}
	return "Local usage: " + strings.Join(parts, ", ")
}

func RenderText(model MenuModel) string {
	lines := []string{}
	if model.AddBox {
		lines = append(lines, "Add a box")
	}
	for _, box := range model.Boxes {
		lines = append(lines, box.Title)
		for _, line := range box.Lines {
			lines = append(lines, "  "+line.Title)
		}
	}
	if model.LocalUsage != nil {
		lines = append(lines, model.LocalUsage.Title)
	}
	lines = append(lines, "Refresh", "Settings", "Quit")
	return strings.Join(lines, "\n")
}
