package simplify

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// FromFile reads an HTML file and returns simplified text.
func FromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read dom file: %w", err)
	}
	return FromString(string(data)), nil
}

// FromString converts HTML string to simplified text.
// Strips UI elements, handles <pre>/<code> with proper newlines,
// and separates block-level content.
func FromString(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}
	var buf strings.Builder
	walkContainer(doc, &buf)
	return normalizeNewlines(buf.String())
}

// walkContainer skips auto-generated html/head/body wrappers and processes content.
func walkContainer(n *html.Node, buf *strings.Builder) {
	if n.Type == html.ElementNode && (n.Data == "html" || n.Data == "head" || n.Data == "body") {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkContainer(c, buf)
		}
		return
	}
	walkNode(n, buf, false)
}

func walkNode(n *html.Node, buf *strings.Builder, inPre bool) {
	switch n.Type {
	case html.TextNode:
		text := n.Data
		if text != "" {
			buf.WriteString(text)
		}
	case html.ElementNode:
		switch n.Data {
		case "pre":
			if !inPre {
				buf.WriteString("\n```\n")
			} else {
				buf.WriteByte('\n')
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkNode(c, buf, true)
			}
			if !inPre {
				buf.WriteString("\n```\n")
			} else {
				buf.WriteByte('\n')
			}
		case "code":
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "br" {
					buf.WriteByte('\n')
				} else {
					walkNode(c, buf, inPre)
				}
			}
		case "p":
			buf.WriteByte('\n')
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkNode(c, buf, inPre)
			}
			buf.WriteByte('\n')
		case "br":
			buf.WriteByte('\n')
		case "h1", "h2", "h3", "h4", "h5", "h6":
			buf.WriteByte('\n')
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkNode(c, buf, inPre)
			}
			buf.WriteByte('\n')
		case "ol":
			i := 1
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "li" {
					fmt.Fprintf(buf, "\n%d. ", i)
					walkChildren(c, buf)
					i++
				}
			}
			buf.WriteByte('\n')
		case "ul":
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "li" {
					buf.WriteString("\n- ")
					walkChildren(c, buf)
				}
			}
			buf.WriteByte('\n')
		case "li":
			walkChildren(n, buf)
		case "table":
			buf.WriteByte('\n')
			writeMarkdownTable(n, buf)
			buf.WriteByte('\n')
		case "div", "section", "article", "main", "header", "footer":
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkNode(c, buf, inPre)
			}
		case "span", "strong", "b", "em", "i", "a", "u", "s", "mark", "sub", "sup":
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkNode(c, buf, inPre)
			}
		case "button", "svg", "img", "input", "textarea", "select", "option",
			"script", "style", "noscript":
			return
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkNode(c, buf, inPre)
			}
		}
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkContainer(c, buf)
		}
	case html.DoctypeNode, html.CommentNode:
		return
	}
}

type tableRow []string

func writeMarkdownTable(n *html.Node, buf *strings.Builder) {
	rows := collectTableRows(n)
	if len(rows) == 0 {
		return
	}

	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	writeMarkdownTableRow(buf, rows[0], width)
	writeMarkdownTableRow(buf, separatorRow(width), width)
	for _, row := range rows[1:] {
		writeMarkdownTableRow(buf, row, width)
	}
}

func collectTableRows(n *html.Node) []tableRow {
	var rows []tableRow
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "tr" {
			if row := collectTableCells(node); len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return rows
}

func collectTableCells(row *html.Node) tableRow {
	var cells tableRow
	for c := row.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "th" || c.Data == "td") {
			cells = append(cells, markdownCellText(c))
		}
	}
	return cells
}

func markdownCellText(n *html.Node) string {
	var buf strings.Builder
	collectPlainText(n, &buf)
	text := strings.Join(strings.Fields(buf.String()), " ")
	return strings.ReplaceAll(text, "|", `\|`)
}

func collectPlainText(n *html.Node, buf *strings.Builder) {
	switch n.Type {
	case html.TextNode:
		buf.WriteString(n.Data)
	case html.ElementNode:
		switch n.Data {
		case "br":
			buf.WriteByte(' ')
		case "button", "svg", "img", "input", "textarea", "select", "option",
			"script", "style", "noscript":
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectPlainText(c, buf)
	}
}

func separatorRow(width int) tableRow {
	row := make(tableRow, width)
	for i := range row {
		row[i] = "---"
	}
	return row
}

func writeMarkdownTableRow(buf *strings.Builder, row tableRow, width int) {
	buf.WriteString("| ")
	for i := 0; i < width; i++ {
		if i > 0 {
			buf.WriteString(" | ")
		}
		if i < len(row) {
			buf.WriteString(row[i])
		}
	}
	buf.WriteString(" |\n")
}

func walkChildren(n *html.Node, buf *strings.Builder) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNode(c, buf, false)
	}
}

var multiNewline = regexp.MustCompile(`\n{3,}`)

func normalizeNewlines(s string) string {
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
