package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

const (
	concurrency = 10
)

var (
	visited = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}
	re        = regexp.MustCompile(`url\(\s*['"]?\s*(https?://[^'")]+?)\s*['"]?\s*\)`)
	logMutex  sync.Mutex
	tasksWg   sync.WaitGroup
	baseURL   *string
	outputDir *string
)

func main() {
	baseURL = flag.String("url", "https://olamundo.pl", "Base URL to start crawling")
	outputDir = flag.String("dir", "./output", "Output directory")
	flag.Parse()

	enqueue := make(chan string)
	dequeue := make(chan string)
	done := make(chan struct{})

	visited.m = make(map[string]bool)

	go enqueueManager(enqueue, dequeue, done)

	enqueueLink(*baseURL, enqueue)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, dequeue, enqueue)
		}(i + 1)
	}

	// Close the enqueue channel after all tasks are completed
	go func() {
		tasksWg.Wait()
		close(enqueue)
	}()

	// Wait for workers to finish
	wg.Wait()

	fmt.Println("Downloading completed.")
}

// enqueueManager manages the task queue
func enqueueManager(enqueue <-chan string, dequeue chan<- string, done <-chan struct{}) {
	queue := []string{}
	for {
		var activeEnqueue <-chan string
		var nextTask string
		var activeDequeue chan<- string

		if len(queue) > 0 {
			activeDequeue = dequeue
			nextTask = queue[0]
		}

		// Allow reading from enqueue regardless of queue length
		activeEnqueue = enqueue

		select {
		case task, ok := <-activeEnqueue:
			if ok {
				queue = append(queue, task)
			} else {
				activeEnqueue = nil
			}
		case activeDequeue <- nextTask:
			queue = queue[1:]
		case <-done:
			return
		}

		// If the enqueue channel is closed and the queue is empty, finish
		if activeEnqueue == nil && len(queue) == 0 {
			close(dequeue)
			return
		}
	}
}

// worker processes tasks from the dequeue channel
func worker(id int, dequeue <-chan string, enqueue chan<- string) {
	for link := range dequeue {
		processLink(link, enqueue)
		tasksWg.Done() // Task completed
	}
}

// enqueueLink adds a link to the queue if it hasn't been processed yet
func enqueueLink(link string, enqueue chan<- string) {
	visited.Lock()
	defer visited.Unlock()
	if visited.m[link] {
		return
	}
	visited.m[link] = true
	tasksWg.Add(1)
	logInfo(fmt.Sprintf("[INFO] Enqueued link: %s\n", link))
	enqueue <- link
}

// processLink checks if the link is a static asset or a page and processes it accordingly
func processLink(link string, enqueue chan<- string) {
	logInfo(fmt.Sprintf("[INFO] Processing link: %s\n", link))
	if isStaticAsset(link) {
		err := downloadAsset(link)
		if err != nil {
			logError(fmt.Sprintf("[ERROR] Failed to download asset: %s - %v\n", link, err))
		}
	} else {
		links, err := processPage(link)
		if err != nil {
			logError(fmt.Sprintf("[ERROR] Failed to process page %s: %v\n", link, err))
			return
		}
		for _, l := range links {
			enqueueLink(l, enqueue)
		}
	}
}

// processPage fetches the HTML page, modifies links, saves it, and returns new links
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

	pageLinks := modifyLinks(doc, pageURL, *baseURL)

	outputFile := getOutputPath(pageURL)
	err = saveHTML(outputFile, doc)
	if err != nil {
		return nil, err
	}

	logInfo(fmt.Sprintf("[INFO] Processed page: %s, found %d new link(s)\n", pageURL, len(pageLinks)))

	return pageLinks, nil
}

// modifyLinks modifies links in the HTML document and returns newly found links
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
						foundLinks = append(foundLinks, absLink.String())
						// If the URL has query parameters, rewrite it
						if absLink.RawQuery != "" {
							if isStaticAsset(absLink.String()) {
								absLink = rewriteAssetURL(absLink)
							} else {
								absLink = rewritePageURL(absLink)
							}
						}
						// Convert link to relative
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

// rewriteAssetURL transforms the static asset URL by integrating query parameters into the file name
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
		// If there's no extension, remove query parameters
		u.RawQuery = ""
		return u
	}

	baseName := strings.TrimSuffix(u.Path, ext)

	// Building suffix based on query parameters
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
	u.RawQuery = "" // remove original query parameters
	return u
}

// rewritePageURL integrates query parameters into the file name for pages
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

// sanitizeQueryPart removes or replaces special characters in parts of query parameters
func sanitizeQueryPart(s string) string {
	s = strings.ReplaceAll(s, "=", "_")
	s = strings.ReplaceAll(s, "?", "_")
	s = strings.ReplaceAll(s, "&", "_")
	return s
}

// resolveURL resolves the link relative to the current URL
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

// processInlineStyle processes the style attribute and returns the modified style and new links
func processInlineStyle(style, currentURL, baseURL string) (string, []string) {
	foundLinks := []string{}
	newStyle := re.ReplaceAllStringFunc(style, func(match string) string {
		urls := re.FindStringSubmatch(match)
		if len(urls) < 2 {
			return match
		}
		originalLink := urls[1]
		absLink, err := resolveURL(currentURL, originalLink)
		if err != nil {
			return match
		}
		if isSameDomain(absLink.String(), baseURL) {
			foundLinks = append(foundLinks, absLink.String())
			if absLink.RawQuery != "" && isStaticAsset(absLink.String()) {
				absLink = rewriteAssetURL(absLink)
			} else if absLink.RawQuery != "" {
				absLink = rewritePageURL(absLink)
			}
			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})
	return newStyle, foundLinks
}

// processInlineCSS processes the CSS content and returns the modified CSS and new links
func processInlineCSS(css, currentURL, baseURL string) (string, []string) {
	foundLinks := []string{}
	newCSS := re.ReplaceAllStringFunc(css, func(match string) string {
		urls := re.FindStringSubmatch(match)
		if len(urls) < 2 {
			return match
		}
		originalLink := urls[1]
		absLink, err := resolveURL(currentURL, originalLink)
		if err != nil {
			return match
		}
		if isSameDomain(absLink.String(), baseURL) {
			foundLinks = append(foundLinks, absLink.String())
			if absLink.RawQuery != "" && isStaticAsset(absLink.String()) {
				absLink = rewriteAssetURL(absLink)
			} else if absLink.RawQuery != "" {
				absLink = rewritePageURL(absLink)
			}
			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})
	return newCSS, foundLinks
}

// convertToRelative converts an absolute link to a relative one relative to baseURL
func convertToRelative(link, base string) string {
	if strings.HasPrefix(link, base) {
		return strings.TrimPrefix(link, base)
	}
	return link
}

// isSameDomain checks if the link belongs to the same domain as baseURL
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

	ext := strings.ToLower(filepath.Ext(u.Path))

	exts := []string{".jpg", ".jpeg", ".png", ".gif", ".css", ".js", ".svg", ".webp", ".woff", ".woff2", ".ttf", ".ico"}
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// downloadAsset downloads the static asset and saves it in the appropriate location
func downloadAsset(link string) error {
	outputFile := getOutputPath(link)

	// Check if the file already exists
	if _, err := os.Stat(outputFile); err == nil {
		logInfo(fmt.Sprintf("[INFO] Asset already exists, skipping download: %s\n", outputFile))
		return nil
	}

	logInfo(fmt.Sprintf("[INFO] Downloading asset: %s\n", link))
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
		logInfo(fmt.Sprintf("[INFO] Saved asset: %s -> %s\n", link, outputFile))
	}
	return err
}

// getOutputPath generates the output path for the given link
func getOutputPath(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(*outputDir, "index.html")
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

	return filepath.Join(*outputDir, path)
}

// saveHTML saves the modified HTML document in the appropriate location
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

// getTextContent retrieves text from the given <style> node
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

// replaceTextContent removes existing text nodes and replaces them with new text
func replaceTextContent(n *html.Node, newText string) {
	// Remove existing text nodes
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.TextNode {
			n.RemoveChild(c)
		}
		c = next
	}
	// Add a new text node
	n.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: newText,
	})
}

// logInfo safely logs information
func logInfo(message string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	fmt.Print(message)
}

// logError safely logs errors
func logError(message string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	fmt.Print(message)
}
