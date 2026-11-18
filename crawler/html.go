package crawler

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// --- HTML processing ---

func processHTML(ctx context.Context, pageURL string, body []byte) ([]string, []string, error) {
	return processHTMLAt(ctx, pageURL, pageURL, body)
}

// processHTMLAt processes a page requested as pageURL whose content was
// served from finalURL (after redirects). Relative links resolve against
// finalURL, as a browser would; the file is saved under pageURL's path.
func processHTMLAt(ctx context.Context, pageURL, finalURL string, body []byte) ([]string, []string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}

	// compute base href, if present
	currentBase := finalURL
	if b := findBaseHref(doc); b != "" {
		if abs, err := resolveURL(finalURL, b); err == nil {
			currentBase = abs.String()
		}
	}

	filterDocument(doc)
	assets, links := rewriteLinks(doc, currentBase, cfg.BaseURL)

	outputFile := cfg.GetOutputPath(pageURL, "text/html")
	write, err := cfg.claimOutput(outputFile, finalURL)
	if err != nil {
		return nil, nil, err
	}
	if !write {
		return assets, links, nil
	}
	if err := saveHTML(outputFile, doc); err != nil {
		return nil, nil, err
	}
	fmt.Printf("[INFO] Saved page: %s -> %s\n", pageURL, outputFile)
	return assets, links, nil
}

func findBaseHref(n *html.Node) string {
	var href string
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "base") {
			for _, a := range node.Attr {
				if strings.EqualFold(a.Key, "href") {
					href = a.Val
					return
				}
			}
		}
		for c := node.FirstChild; c != nil && href == ""; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return href
}

func filterDocument(n *html.Node) {
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Remove unwanted tags (links with specific rel attributes)
			for _, unwanted := range unwantedTags {
				if strings.EqualFold(node.Data, unwanted.Tag) {
					match := true
					for key, val := range unwanted.Attrs {
						found := false
						for _, attr := range node.Attr {
							if strings.EqualFold(attr.Key, key) && strings.EqualFold(attr.Val, val) {
								found = true
								break
							}
						}
						if !found {
							match = false
							break
						}
					}
					if match && node.Parent != nil {
						node.Parent.RemoveChild(node)
						return
					}
				}
			}

			// Remove unwanted scripts
			if strings.EqualFold(node.Data, "script") && shouldRemoveScript(node) {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
					return
				}
			}

			// Remove unwanted styles (WordPress emoji CSS)
			if strings.EqualFold(node.Data, "style") && shouldRemoveStyle(node) {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
					return
				}
			}
		}

		// Remove all HTML comments (including CDATA sections)
		if node.Type == html.CommentNode {
			if node.Parent != nil {
				node.Parent.RemoveChild(node)
				return
			}
		}

		for c := node.FirstChild; c != nil; {
			next := c.NextSibling
			f(c)
			c = next
		}
	}
	f(n)
}

// shouldRemoveScript checks if a script node should be removed
// Returns true for Cloudflare challenge scripts, comment-reply scripts, and WordPress emoji handler
func shouldRemoveScript(node *html.Node) bool {
	// Check for unwanted scripts in src attribute
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "src") {
			if strings.Contains(attr.Val, "comment-reply.min.js") ||
				strings.Contains(attr.Val, "wp-emoji-release.min.js") {
				return true
			}
		}
	}

	// Check for unwanted inline scripts
	if node.FirstChild != nil && node.FirstChild.Type == html.TextNode {
		content := node.FirstChild.Data

		// Cloudflare challenge script
		if strings.Contains(content, "cdn-cgi/challenge-platform") ||
			strings.Contains(content, "__CF$cv$params") ||
			strings.Contains(content, "window.__CF$cv$params") {
			return true
		}

		// WordPress emoji handler script
		// Detects: window._wpemojiSettings = {...}
		if strings.Contains(content, "window._wpemojiSettings") ||
			strings.Contains(content, "wp-emoji-release.min.js") {
			return true
		}
	}

	return false
}

// shouldRemoveStyle checks if a style node should be removed
// Returns true for WordPress emoji CSS
func shouldRemoveStyle(node *html.Node) bool {
	// Check for WordPress emoji styles by id attribute
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "id") {
			// WordPress emoji inline styles
			if strings.Contains(attr.Val, "wp-emoji-styles") {
				return true
			}
		}
	}

	// Also check content for wp-smiley/emoji classes (secondary check)
	if node.FirstChild != nil && node.FirstChild.Type == html.TextNode {
		content := node.FirstChild.Data
		if strings.Contains(content, "img.wp-smiley") && strings.Contains(content, "img.emoji") {
			return true
		}
	}

	return false
}

func rewriteLinks(n *html.Node, currentURL, base string) ([]string, []string) {
	var assets []string
	var links []string
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Process attributes
			nodeAssets, nodeLinks := processNodeAttributes(node, currentURL, base)
			assets = append(assets, nodeAssets...)
			links = append(links, nodeLinks...)

			// Process special elements
			specialAssets := processSpecialElements(node, currentURL, base)
			assets = append(assets, specialAssets...)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return assets, links
}

// processNodeAttributes handles all attributes (href, src, style, srcset) for a node
func processNodeAttributes(node *html.Node, currentURL, base string) ([]string, []string) {
	var assets []string
	var links []string

	for i := range node.Attr {
		attr := &node.Attr[i]
		switch strings.ToLower(attr.Key) {
		case "href":
			nodeAssets, nodeLinks := processHrefAttribute(node, attr, currentURL, base)
			assets = append(assets, nodeAssets...)
			links = append(links, nodeLinks...)
		case "src":
			nodeAssets := processSrcAttribute(attr, currentURL, base)
			assets = append(assets, nodeAssets...)
		case "style":
			nodeAssets := processStyleAttribute(attr, currentURL, base)
			assets = append(assets, nodeAssets...)
		case "srcset":
			nodeAssets := processSrcsetAttribute(attr, currentURL, base)
			assets = append(assets, nodeAssets...)
		}
	}
	return assets, links
}

// processHrefAttribute handles href attributes (links or assets depending on element type)
func processHrefAttribute(node *html.Node, attr *html.Attribute, currentURL, base string) ([]string, []string) {
	var assets []string
	var links []string

	// href is usually a link, unless it's a <link> tag for CSS/icon
	isAsset, isCanonical := false, false
	if strings.EqualFold(node.Data, "link") {
		// check rel
		for _, a := range node.Attr {
			if strings.EqualFold(a.Key, "rel") {
				val := strings.ToLower(a.Val)
				if strings.Contains(val, "stylesheet") || strings.Contains(val, "icon") {
					isAsset = true
				}
				for _, tok := range strings.Fields(val) {
					if tok == "canonical" {
						isCanonical = true
					}
				}
				break
			}
		}
	}

	orig := attr.Val
	if isFragmentOnly(orig) {
		return nil, nil
	}
	abs, err := resolveURL(currentURL, orig)
	if err == nil && cfg.SameHost(abs.String(), base) {
		if isAsset {
			assets = append(assets, urlKey(abs))
		} else {
			links = append(links, urlKey(abs))
		}
		fixPath(abs)
		if isCanonical {
			attr.Val = cfg.ToPublicURL(abs, base)
		} else {
			attr.Val = cfg.ToRelative(abs, base)
		}
	}
	return assets, links
}

// processSrcAttribute handles src attributes (always assets)
func processSrcAttribute(attr *html.Attribute, currentURL, base string) []string {
	var assets []string

	orig := attr.Val
	if isFragmentOnly(orig) {
		return nil
	}
	abs, err := resolveURL(currentURL, orig)
	if err == nil && cfg.SameHost(abs.String(), base) {
		assets = append(assets, urlKey(abs))
		fixPath(abs)
		attr.Val = cfg.ToRelative(abs, base)
	}
	return assets
}

// processStyleAttribute handles inline style attributes
func processStyleAttribute(attr *html.Attribute, currentURL, base string) []string {
	newStyle, found := cfg.ProcessInlineStyle(attr.Val, currentURL, base)
	attr.Val = newStyle
	return found
}

// processSrcsetAttribute handles srcset attributes
func processSrcsetAttribute(attr *html.Attribute, currentURL, base string) []string {
	newSrc, found := cfg.ProcessSrcSet(attr.Val, currentURL, base)
	attr.Val = newSrc
	return found
}

// processSpecialElements handles <style>, <script>, and <source> elements
func processSpecialElements(node *html.Node, currentURL, base string) []string {
	var assets []string

	switch strings.ToLower(node.Data) {
	case "style":
		assets = append(assets, processStyleElement(node, currentURL, base)...)
	case "script":
		assets = append(assets, processScriptElement(node, currentURL, base)...)
	}
	return assets
}

// processStyleElement handles <style>...</style> elements
func processStyleElement(node *html.Node, currentURL, base string) []string {
	css := getTextContent(node)
	// Remove CSS comments (/* ... */) and CDATA markers
	css = removeCSSComments(css)
	css = removeCDATAMarkers(css)
	newCSS, found := cfg.ProcessInlineCSS(css, currentURL, base)
	replaceTextContent(node, newCSS)
	return found
}

// processScriptElement handles <script>...</script> elements (only Simple Lightbox scripts)
func processScriptElement(node *html.Node, currentURL, base string) []string {
	var scriptID string
	for _, a := range node.Attr {
		if strings.EqualFold(a.Key, "id") {
			scriptID = a.Val
			break
		}
	}
	// Only process specific Simple Lightbox scripts to avoid breaking other JS
	if scriptID == "slb_footer" || scriptID == "slb_context" {
		jsContent := getTextContent(node)
		// Remove CDATA markers from JavaScript
		jsContent = removeCDATAMarkers(jsContent)
		newJS, found := cfg.ProcessInlineJS(jsContent, currentURL, base)
		replaceTextContent(node, newJS)
		return found
	}
	return nil
}
