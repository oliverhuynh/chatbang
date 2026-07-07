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
