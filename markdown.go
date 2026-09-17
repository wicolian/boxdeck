package main

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

var inlineRE = regexp.MustCompile("!\\[([^\\]]*)\\]\\(([^\\s)]+)\\)|\\[([^\\]]+)\\]\\(([^\\s)]+)\\)|`([^`]+)`|\\*\\*([^*]+)\\*\\*|__([^_]+)__|\\*([^*]+)\\*|_([^_]+)_")
var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
var listRE = regexp.MustCompile(`^\s*(?:[-+*]|\d+\.)\s+(.+)$`)
var orderedRE = regexp.MustCompile(`^\s*\d+\.`)
var separatorRE = regexp.MustCompile(`^:?-{3,}:?$`)

func safeMarkdownURL(s string, image bool) string {
	u, err := url.Parse(html.UnescapeString(s))
	if err != nil {
		return "#"
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https":
		return s
	case "mailto":
		if !image {
			return s
		}
	}
	return "#"
}

// Raw HTML is escaped. Inline matching works on the source once, so generated
// attributes never go back through emphasis parsing or entity decoding.
func inline(md string) string {
	var out strings.Builder
	at := 0
	for _, m := range inlineRE.FindAllStringSubmatchIndex(md, -1) {
		out.WriteString(html.EscapeString(md[at:m[0]]))
		get := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return md[m[2*i]:m[2*i+1]]
		}
		switch {
		case m[2] >= 0:
			out.WriteString(`<img alt="` + html.EscapeString(get(1)) + `" src="` + html.EscapeString(safeMarkdownURL(get(2), true)) + `">`)
		case m[6] >= 0:
			out.WriteString(`<a href="` + html.EscapeString(safeMarkdownURL(get(4), false)) + `">` + html.EscapeString(get(3)) + `</a>`)
		case m[10] >= 0:
			out.WriteString("<code>" + html.EscapeString(get(5)) + "</code>")
		case m[12] >= 0:
			out.WriteString("<strong>" + html.EscapeString(get(6)) + "</strong>")
		case m[14] >= 0:
			out.WriteString("<strong>" + html.EscapeString(get(7)) + "</strong>")
		case m[16] >= 0:
			out.WriteString("<em>" + html.EscapeString(get(8)) + "</em>")
		default:
			out.WriteString("<em>" + html.EscapeString(get(9)) + "</em>")
		}
		at = m[1]
	}
	out.WriteString(html.EscapeString(md[at:]))
	return out.String()
}
func tableCells(s string) []string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(s), "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}
func tableSeparator(s string) bool {
	if !strings.Contains(s, "|") {
		return false
	}
	for _, cell := range tableCells(s) {
		if !separatorRE.MatchString(cell) {
			return false
		}
	}
	return true
}

// Deliberately small Markdown subset: paragraphs, headings, flat lists, fences,
// inline code/emphasis/links/images and tables. No raw HTML or executable URLs.
func markdown(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		s := strings.TrimSpace(lines[i])
		if s == "" {
			i++
			continue
		}
		if strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") {
			fence := s[:3]
			i++
			out.WriteString("<pre><code>")
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
				out.WriteString(html.EscapeString(lines[i]) + "\n")
				i++
			}
			if i < len(lines) {
				i++
			}
			out.WriteString("</code></pre>\n")
			continue
		}
		if m := headingRE.FindStringSubmatch(s); m != nil {
			tag := string(rune('0' + len(m[1])))
			out.WriteString("<h" + tag + ">" + inline(m[2]) + "</h" + tag + ">\n")
			i++
			continue
		}
		if i+1 < len(lines) && strings.Contains(s, "|") && tableSeparator(lines[i+1]) {
			out.WriteString("<table><thead><tr>")
			for _, cell := range tableCells(s) {
				out.WriteString("<th>" + inline(cell) + "</th>")
			}
			out.WriteString("</tr></thead><tbody>")
			i += 2
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				out.WriteString("<tr>")
				for _, cell := range tableCells(lines[i]) {
					out.WriteString("<td>" + inline(cell) + "</td>")
				}
				out.WriteString("</tr>")
				i++
			}
			out.WriteString("</tbody></table>\n")
			continue
		}
		if listRE.MatchString(s) {
			tag := "ul"
			ordered := orderedRE.MatchString(s)
			if ordered {
				tag = "ol"
			}
			out.WriteString("<" + tag + ">")
			for i < len(lines) {
				m := listRE.FindStringSubmatch(lines[i])
				if m == nil || orderedRE.MatchString(lines[i]) != ordered {
					break
				}
				out.WriteString("<li>" + inline(m[1]) + "</li>")
				i++
			}
			out.WriteString("</" + tag + ">\n")
			continue
		}
		paragraph := []string{s}
		i++
		for i < len(lines) {
			next := strings.TrimSpace(lines[i])
			if next == "" || headingRE.MatchString(next) || listRE.MatchString(next) || strings.HasPrefix(next, "```") || strings.HasPrefix(next, "~~~") || (i+1 < len(lines) && tableSeparator(lines[i+1])) {
				break
			}
			paragraph = append(paragraph, next)
			i++
		}
		out.WriteString("<p>" + inline(strings.Join(paragraph, "\n")) + "</p>\n")
	}
	return out.String()
}
