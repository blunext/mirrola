package main

import (
	"bytes"
	"errors"
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
	"sync/atomic"

	"golang.org/x/net/html"
)

const (
	concurrency = 10
	queueSize   = 10000
)

var (
	visited = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}
	re         = regexp.MustCompile(`url\(\s*['"]?\s*(https?://[^'")]+?)\s*['"]?\s*\)`)
	tasksWg    sync.WaitGroup
	baseURL    *string
	outputDir  *string
	rewriteUrl *bool
)

type unwantedTag struct {
	Tag   string
	Attrs map[string]string
}

var unwantedTags = []unwantedTag{
	{ // <link rel="shortlink" href="/?p=1019">
		Tag: "link", Attrs: map[string]string{"rel": "shortlink"},
	},
	{ // <link rel="pingback" href="/xmlrpc.php">
		Tag: "link", Attrs: map[string]string{"rel": "pingback"},
	},
	{ // <link rel="EditURI" type="application/rsd+xml" title="RSD" href="/xmlrpc.php?rsd">
		Tag: "link", Attrs: map[string]string{"rel": "EditURI"},
	},
	{ // <link rel="https://api.w.org/" href="/wp-json/">
		Tag: "link", Attrs: map[string]string{"rel": "https://api.w.org/"},
	},
	{ // <link rel="alternate" title="JSON" type="application/json" href="/wp-json/wp/v2/posts/1019">
		Tag: "link", Attrs: map[string]string{"rel": "alternate", "title": "JSON", "type": "application/json"},
	},
	{ // <link rel="alternate" title="oEmbed (JSON)" type="application/json+oembed" href="...">
		Tag: "link", Attrs: map[string]string{"rel": "alternate", "type": "application/json+oembed"},
	},
	{ // <link rel="alternate" title="oEmbed (XML)" type="text/xml+oembed" href="...">
		Tag: "link", Attrs: map[string]string{"rel": "alternate", "type": "text/xml+oembed"},
	},
}

func main() {
	var processError atomic.Value
	baseURL = flag.String("url", "https://olamundo.pl", "Base URL to start crawling")
	outputDir = flag.String("dir", "./output", "Output directory")
	rewriteUrl = flag.Bool("rewrite", false, "Rewrite URLs based on query parameters")
	flag.Parse()

	tasks := make(chan string, queueSize)

	enqueueLink(*baseURL, tasks)

	go func() {
		tasksWg.Wait()
		close(tasks)
	}()

	var workersWg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workersWg.Add(1)
		go func() {
			defer workersWg.Done()
			for link := range tasks {
				if err := processLink(link, tasks); err != nil {
					fmt.Printf("[ERROR] Failed to process link: %s - %v\n", link, err)
					if processError.Load() == nil {
						processError.Store(err)
					} else {
						processError.Store(fmt.Errorf("%v; %v", processError.Load(), err))
					}
					return
				}
				tasksWg.Done()
			}
		}()
	}

	// Wait for all workers to finish
	workersWg.Wait()

	if err := processError.Load(); err != nil {
		fmt.Printf("[ERROR] Processing failed: %v\n", err)
		return
	}
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
	tasks <- link
	//fmt.Printf("[INFO] Enqueued link: %s\n", link)
}

// processLink checks if the link is an asset or a page and processes it accordingly
func processLink(link string, tasks chan<- string) error {
	//fmt.Printf("[INFO] Processing link: %s, queue size: %d\n", link, len(tasks))
	if isStaticAsset(link) {
		err := downloadAsset(link)
		if err != nil {
			fmt.Printf("[ERROR] Failed to download asset: %s - %v\n", link, err)
			return err
		}
		return nil
	}
	links, err := processPage(link)
	if err != nil {
		fmt.Printf("[ERROR] Failed to process page %s: %v\n", link, err)
		return err
	}
	if len(tasks)+len(links) > queueSize {
		fmt.Printf("[WARN] Queue is full, no of links: %d\n", len(tasks)+len(links))
		return errors.New("[ERROR] queue is full")
	}
	fmt.Printf("[INFO] Processed page: %s, found %d new links, queue size: %d\n", link, len(links), len(tasks))
	for _, l := range links {
		// we use goroutine to avoid deadlock when the channel is full
		enqueueLink(l, tasks)
	}
	return nil
}

// processPage downloads the HTML page, filters unwanted tags, modifies links, saves it, and returns newly found links
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

	filterDocument(doc)

	pageLinks := modifyLinks(doc, pageURL, *baseURL)

	outputFile := getOutputPath(pageURL)
	err = saveHTML(outputFile, doc)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[INFO] Saved page: %s -> %s\n", pageURL, outputFile)
	return pageLinks, nil
}

// filterDocument removes unwanted HTML tags based on the predefined list.
func filterDocument(n *html.Node) {
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, unwanted := range unwantedTags {
				if strings.ToLower(node.Data) == unwanted.Tag {
					match := true
					for key, val := range unwanted.Attrs {
						found := false
						for _, attr := range node.Attr {
							if strings.ToLower(attr.Key) == key && strings.ToLower(attr.Val) == strings.ToLower(val) {
								found = true
								break
							}
						}
						if !found {
							match = false
							break
						}
					}
					if match {
						// Remove this node from its parent
						if node.Parent != nil {
							node.Parent.RemoveChild(node)
							//fmt.Printf("[INFO] Removed unwanted tag: <%s ", node.Data)
							//for _, attr := range node.Attr {
							//	fmt.Printf(`%s="%s" `, attr.Key, attr.Val)
							//}
							//fmt.Println(">")
							return
						}
					}
				}
			}
		}
		// Continue traversing the node tree
		for c := node.FirstChild; c != nil; {
			next := c.NextSibling
			f(c)
			c = next
		}
	}
	f(n)
}

// modifyLinks checks all relevant attributes and modifies links accordingly
func modifyLinks(n *html.Node, currentURL, baseURL string) []string {
	var foundLinks []string

	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Handle href, src, style and srcset attributes
			for i, attr := range node.Attr {
				switch attr.Key {
				case "href", "src":
					original := attr.Val
					// Convert link to absolute
					absLink, err := resolveURL(currentURL, original)
					if err == nil && isSameDomain(absLink.String(), baseURL) {
						foundLinks = append(foundLinks, absLink.String())
						// If URL has query parameters, rewrite based on whether it's an asset or page
						if *rewriteUrl && absLink.RawQuery != "" {
							if isStaticAsset(absLink.String()) {
								absLink = rewriteAssetURL(absLink)
							} else {
								absLink = rewritePageURL(absLink)
							}
						}
						// Replace link with relative
						rel := convertToRelative(absLink.String(), baseURL)
						node.Attr[i].Val = rel
					}
				case "style":
					newStyle, styleLinks := processInlineStyle(attr.Val, currentURL, baseURL)
					node.Attr[i].Val = newStyle
					foundLinks = append(foundLinks, styleLinks...)
				case "srcset":
					newSrcSet, srcSetLinks := processSrcSet(attr.Val, currentURL, baseURL)
					node.Attr[i].Val = newSrcSet
					foundLinks = append(foundLinks, srcSetLinks...)
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

// processSrcSet analyzes and processes the srcset attribute
// srcset = "https://domain.com/img/image-300w.jpg 300w, https://domain.com/img/image-600w.jpg 600w, https://domain.com/img/image-2x.jpg 2x"
// Processed srcset = "/img/image-300w.jpg 300w, /img/image-600w.jpg 600w, /img/image-2x.jpg 2x"
func processSrcSet(srcset string, currentURL, baseURL string) (string, []string) {
	var foundLinks []string
	var newSrcSetParts []string

	// Split srcset into individual sources
	parts := strings.Split(srcset, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Separate URL and descriptor (e.g., "2x", "300w")
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		imageURL := fields[0]
		descriptor := ""
		if len(fields) > 1 {
			descriptor = fields[1]
		}

		// Process the URL
		absLink, err := resolveURL(currentURL, imageURL)
		if err != nil {
			// If the URL cannot be processed, keep the original
			newSrcSetParts = append(newSrcSetParts, part)
			continue
		}

		if isSameDomain(absLink.String(), baseURL) {
			foundLinks = append(foundLinks, absLink.String())
			// Rewrite the URL if required
			if *rewriteUrl && absLink.RawQuery != "" {
				if isStaticAsset(absLink.String()) {
					absLink = rewriteAssetURL(absLink)
				} else {
					absLink = rewritePageURL(absLink)
				}
			}
			relativeURL := convertToRelative(absLink.String(), baseURL)
			if descriptor != "" {
				newSrcSetParts = append(newSrcSetParts, fmt.Sprintf("%s %s", relativeURL, descriptor))
			} else {
				newSrcSetParts = append(newSrcSetParts, relativeURL)
			}
		} else {
			// If the domain is different, keep the original
			newSrcSetParts = append(newSrcSetParts, part)
		}
	}

	newSrcSet := strings.Join(newSrcSetParts, ", ")
	return newSrcSet, foundLinks
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
		originalLink := urls[1]
		absLink, err := resolveURL(currentURL, originalLink)
		if err != nil {
			return match
		}
		if isSameDomain(absLink.String(), baseURL) {
			foundLinks = append(foundLinks, absLink.String())
			if *rewriteUrl && absLink.RawQuery != "" {
				if isStaticAsset(absLink.String()) {
					absLink = rewriteAssetURL(absLink)
				} else {
					absLink = rewritePageURL(absLink)
				}
			}
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
		originalLink := urls[1]
		absLink, err := resolveURL(currentURL, originalLink)
		if err != nil {
			return match
		}
		if isSameDomain(absLink.String(), baseURL) {
			foundLinks = append(foundLinks, absLink.String())
			if *rewriteUrl && absLink.RawQuery != "" {
				if isStaticAsset(absLink.String()) {
					absLink = rewriteAssetURL(absLink)
				} else {
					absLink = rewritePageURL(absLink)
				}
			}
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
		//fmt.Printf("[INFO] Asset already exists: %s\n", outputFile)
		return nil
	}

	//fmt.Printf("[INFO] Downloading asset: %s\n", link)
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
	if *rewriteUrl && u.RawQuery != "" {
		if isStaticAsset(u.String()) {
			u = rewriteAssetURL(u)
		} else {
			u = rewritePageURL(u)
		}
		path = u.Path
	}

	return filepath.Join(*outputDir, path)
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
