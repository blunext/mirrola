package crawler

import (
	"strings"

	"golang.org/x/net/html"
)

// --- HTML text content helpers ---

func getTextContent(n *html.Node) string {
	var sb strings.Builder
	var g func(*html.Node)
	g = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			g(c)
		}
	}
	g(n)
	return sb.String()
}

func replaceTextContent(n *html.Node, newText string) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.TextNode {
			n.RemoveChild(c)
		}
		c = next
	}
	n.AppendChild(&html.Node{Type: html.TextNode, Data: newText})
}
