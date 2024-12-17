package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	baseURL     = "https://olamundo.pl"
	outputDir   = "./output"
	concurrency = 10
	//sleepTime   = 2 * time.Second
	sleepTime = time.Nanosecond
)

var (
	visited = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}
)
var (
	tasksWg sync.WaitGroup
	re      = regexp.MustCompile(`url\(['"]?(https?://[^'")]+)['"]?\)`)
)

func main() {
	tasks := make(chan string, 10000)

	// Goroutine that waits for tasksWg to reach 0 and then closes the channel
	go func() {
		tasksWg.Wait()
		close(tasks)
	}()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for link := range tasks {
				processLink(link, tasks)
				tasksWg.Done() // Task completed
			}
		}()
	}

	// Add the first link (base page)
	enqueueLink(baseURL, tasks)

	// Wait for all workers to finish
	wg.Wait()

	fmt.Println("Downloading completed.")
}

// enqueueLink adds a link to the channel if it has not been processed yet.
func enqueueLink(link string, tasks chan<- string) {
	visited.Lock()
	defer visited.Unlock()
	if visited.m[link] {
		return
	}
	visited.m[link] = true
	tasksWg.Add(1)
	fmt.Printf("[INFO] Enqueued link: %s\n", link)
	tasks <- link
}

// processLink checks if the link is an asset or a page and processes it accordingly
func processLink(link string, tasks chan<- string) {
	fmt.Printf("[INFO] Processing link: %s\n", link)
	if isStaticAsset(link) {
		err := downloadAsset(link)
		if err != nil {
			fmt.Printf("[ERROR] Failed to download asset: %s - %v\n", link, err)
		}
	} else {
		time.Sleep(sleepTime)
		links, err := processPage(link)
		if err != nil {
			fmt.Printf("[ERROR] Failed to process page %s: %v\n", link, err)
			return
		}
		for _, l := range links {
			go enqueueLink(l, tasks)
		}
	}
}

// processPage downloads the HTML page, modifies links, saves it, and returns newly found links
func processPage(pageURL string) ([]string, error) {
	resp, err := http.Get(pageURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	pageLinks := modifyLinks(doc, pageURL, baseURL)

	outputFile := getOutputPath(pageURL)
	err = saveHTML(outputFile, doc)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[INFO] Processed page: %s, found %d new link(s)\n", pageURL, len(pageLinks))

	return pageLinks, nil
}

func modifyLinks(n *html.Node, currentURL, baseURL string) []string {
	var foundLinks []string

	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Handle href, src, style attributes
			for i, attr := range node.Attr {
				if attr.Key == "href" || attr.Key == "src" {
					original := attr.Val
					// Convert link to absolute
					absLink, err := resolveURL(currentURL, original)
					if err == nil && isSameDomain(absLink.String(), baseURL) {
						// If URL has query parameters, rewrite based on whether it's an asset or page
						if absLink.RawQuery != "" {
							if isStaticAsset(absLink.String()) {
								absLink = rewriteAssetURL(absLink)
							} else {
								absLink = rewritePageURL(absLink)
							}
						}
						foundLinks = append(foundLinks, absLink.String())
						// Replace link with relative
						rel := convertToRelative(absLink.String(), baseURL)
						node.Attr[i].Val = rel
					}
				}
				if attr.Key == "style" {
					newStyle, styleLinks := processInlineStyle(attr.Val, currentURL, baseURL)
					node.Attr[i].Val = newStyle
					foundLinks = append(foundLinks, styleLinks...)
				}
			}

			// Additional handling for <style>...</style>
			if node.Data == "style" {
				cssContent := getTextContent(node)
				newCSS, cssLinks := processInlineCSS(cssContent, currentURL, baseURL)
				replaceTextContent(node, newCSS)
				foundLinks = append(foundLinks, cssLinks...)
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)

	return foundLinks
}

// rewriteAssetURL transforms an asset URL by incorporating query parameters into the filename.
func rewriteAssetURL(u *url.URL) *url.URL {
	if u.RawQuery == "" {
		return u
	}

	q := u.Query()
	if len(q) == 0 {
		u.RawQuery = ""
		return u
	}

	ext := filepath.Ext(u.Path)
	if ext == "" {
		// If no extension, just drop the query parameters
		u.RawQuery = ""
		return u
	}

	baseName := strings.TrimSuffix(u.Path, ext)

	// Build a suffix based on query parameters
	var parts []string
	for key, values := range q {
		for _, val := range values {
			safeKey := sanitizeQueryPart(key)
			safeVal := sanitizeQueryPart(val)
			parts = append(parts, safeKey+"_"+safeVal)
		}
	}
	suffix := strings.Join(parts, "_")

	u.Path = baseName + "_" + suffix + ext
	u.RawQuery = "" // remove original query
	return u
}

// rewritePageURL integrates query parameters into the filename for pages.
// Example: /?p=111 -> /index_p_111.html
func rewritePageURL(u *url.URL) *url.URL {
	if u.RawQuery == "" {
		return u
	}

	ext := filepath.Ext(u.Path)
	if ext == "" {
		if u.Path == "" || u.Path == "/" {
			u.Path = "/index.html"
			ext = ".html"
		} else {
			if !strings.HasSuffix(u.Path, "/") {
				u.Path += "/"
			}
			u.Path += "index.html"
			ext = ".html"
		}
	}

	dir := filepath.Dir(u.Path)
	base := filepath.Base(u.Path)
	name := strings.TrimSuffix(base, ext)

	safeQuery := u.RawQuery
	safeQuery = strings.ReplaceAll(safeQuery, "&", "_")
	safeQuery = strings.ReplaceAll(safeQuery, "=", "_")
	safeQuery = strings.ReplaceAll(safeQuery, "?", "_")

	base = name + "_" + safeQuery + ext
	u.Path = filepath.Join(dir, base)
	u.RawQuery = ""

	return u
}

func sanitizeQueryPart(s string) string {
	s = strings.ReplaceAll(s, "=", "_")
	s = strings.ReplaceAll(s, "?", "_")
	s = strings.ReplaceAll(s, "&", "_")
	return s
}

func resolveURL(currentURL, link string) (*url.URL, error) {
	base, err := url.Parse(currentURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(u), nil
}

func processInlineStyle(style, currentURL, baseURL string) (string, []string) {
	foundLinks := []string{}
	newStyle := re.ReplaceAllStringFunc(style, func(match string) string {
		urls := re.FindStringSubmatch(match)
		if len(urls) < 2 {
			return match
		}
		u := urls[1]
		absLink, err := resolveURL(currentURL, u)
		if err != nil {
			return match
		}
		if isSameDomain(absLink.String(), baseURL) {
			if absLink.RawQuery != "" && isStaticAsset(absLink.String()) {
				absLink = rewriteAssetURL(absLink)
			} else if absLink.RawQuery != "" {
				absLink = rewritePageURL(absLink)
			}
			foundLinks = append(foundLinks, absLink.String())
			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})
	return newStyle, foundLinks
}

func processInlineCSS(css, currentURL, baseURL string) (string, []string) {
	foundLinks := []string{}
	newCSS := re.ReplaceAllStringFunc(css, func(match string) string {
		urls := re.FindStringSubmatch(match)
		if len(urls) < 2 {
			return match
		}
		u := urls[1]
		absLink, err := resolveURL(currentURL, u)
		if err != nil {
			return match
		}
		if isSameDomain(absLink.String(), baseURL) {
			if absLink.RawQuery != "" && isStaticAsset(absLink.String()) {
				absLink = rewriteAssetURL(absLink)
			} else if absLink.RawQuery != "" {
				absLink = rewritePageURL(absLink)
			}
			foundLinks = append(foundLinks, absLink.String())
			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})
	return newCSS, foundLinks
}

func convertToRelative(link, base string) string {
	if strings.HasPrefix(link, base) {
		return strings.TrimPrefix(link, base)
	}
	return link
}

func isSameDomain(link, base string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	bu, err := url.Parse(base)
	if err != nil {
		return false
	}
	return u.Host == bu.Host
}

// isStaticAsset checks if the link points to a known static asset extension
func isStaticAsset(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	// We only check the extension of the path, ignoring query parameters.
	ext := strings.ToLower(filepath.Ext(u.Path))

	exts := []string{".jpg", ".jpeg", ".png", ".gif", ".css", ".js", ".svg", ".webp", ".woff", ".woff2", ".ttf", ".ico"}
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

func downloadAsset(link string) error {
	outputFile := getOutputPath(link)

	// Check if the file already exists
	if _, err := os.Stat(outputFile); err == nil {
		fmt.Printf("[INFO] Asset already exists, skipping download: %s\n", outputFile)
		return nil
	}

	fmt.Printf("[INFO] Downloading asset: %s\n", link)
	resp, err := http.Get(link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = os.MkdirAll(filepath.Dir(outputFile), 0755)
	if err != nil {
		return err
	}

	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err == nil {
		fmt.Printf("[INFO] Saved asset: %s -> %s\n", link, outputFile)
	}
	return err
}

func getOutputPath(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(outputDir, "index.html")
	}

	path := u.Path
	if path == "" || path == "/" {
		path = "/index.html"
	} else {
		if strings.HasSuffix(path, "/") {
			path = path + "index.html"
		} else {
			ext := filepath.Ext(path)
			if ext == "" {
				path = path + "/index.html"
			}
		}
	}

	// If there are query parameters, rewrite the URL accordingly
	if u.RawQuery != "" {
		if isStaticAsset(u.String()) {
			u = rewriteAssetURL(u)
		} else {
			u = rewritePageURL(u)
		}
		path = u.Path
	}

	return filepath.Join(outputDir, path)
}

func saveHTML(outputFile string, doc *html.Node) error {
	err := os.MkdirAll(filepath.Dir(outputFile), 0755)
	if err != nil {
		return err
	}

	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	return html.Render(file, doc)
}

// getTextContent retrieves text from a given <style> node
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

// replaceTextContent removes existing text children and replaces them with new text
func replaceTextContent(n *html.Node, newText string) {
	// Remove existing text children
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.TextNode {
			n.RemoveChild(c)
		}
		c = next
	}
	// Add new text node
	n.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: newText,
	})
}
