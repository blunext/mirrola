package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	queueSize = 10000
)

var (
	concurrency = runtime.NumCPU()
	visited     = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}
	re         = regexp.MustCompile(`url\(\s*['"]?\s*([^'")]+?)\s*['"]?\s*\)`) // Example regex that captures url('...') or url("...") or url(...)
	reJS       = regexp.MustCompile(`(https?://[^\s"']+)`)                     // Example regex that captures http:// or https:// up to the first whitespace or quote.
	unicodeEsc = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`)                       // Example regex that captures \uXXXX unicode escapes.
	tasksWg    sync.WaitGroup
	baseURL    *string
	outputDir  *string
	rewriteUrl *bool
)

// Unwanted tags that we'll remove (e.g. <link rel="shortlink" ...>)
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

	baseURL = flag.String("url", "", "Base URL to start crawling")
	outputDir = flag.String("dir", "./output", "Output directory")
	rewriteUrl = flag.Bool("rewrite", false, "Rewrite URLs based on query parameters")
	flag.Parse()

	if *baseURL == "" {
		fmt.Println("Error: The -url parameter is required.")
		flag.Usage()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Starting download for %s, number of workers: %d\n", *baseURL, concurrency)
	tasks := make(chan string, queueSize)

	enqueueLink(ctx, *baseURL, tasks)

	go func() {
		// Wait for all tasks to be done or context to be canceled
		tasksWg.Wait()
		cancel() // In case all tasks are done without error
		close(tasks)
	}()

	var workersWg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workersWg.Add(1)
		go func() {
			defer workersWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case link, ok := <-tasks:
					if !ok {
						return
					}
					if err := processLink(ctx, link, tasks); err != nil {
						fmt.Printf("[ERROR] Failed to process link: %s - %v\n", link, err)
						if processError.Load() == nil {
							processError.Store(err)
							cancel()
						}
						return
					}
					tasksWg.Done()
				}
			}
		}()
	}

	// Wait for all workers to finish
	workersWg.Wait()

	// Check if there was an error
	if err, ok := processError.Load().(error); ok && err != nil {
		fmt.Printf("[ERROR] Processing failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Downloading completed.")
}

// enqueueLink adds a link to the channel if it has not been processed yet and context is not canceled.
func enqueueLink(ctx context.Context, link string, tasks chan<- string) error {
	visited.Lock()
	defer visited.Unlock()

	if visited.m[link] {
		return nil
	}
	visited.m[link] = true
	tasksWg.Add(1)

	select {
	case <-ctx.Done():
		// Do not enqueue if context is canceled
		tasksWg.Done()
		return fmt.Errorf("[ERROR] Context canceled while enqueueing link: %s", link)
	case tasks <- link: // Enqueued successfully
		//fmt.Printf("[INFO] Enqueued link: %s\n", link)
		return nil
	default:
		// channel is full - report an error
		tasksWg.Done()
		return fmt.Errorf("[ERROR] Queue is full, cannot enqueue link: %s", link)
	}
}

// processLink checks if the link is an asset or a page and processes it accordingly
func processLink(ctx context.Context, link string, tasks chan<- string) error {
	//fmt.Printf("[INFO] Processing link: %s, queue size: %d\n", link, len(tasks))
	if isStaticAsset(link) {
		if err := downloadAsset(ctx, link, tasks); err != nil {
			fmt.Printf("[ERROR] Failed to download asset: %s - %v\n", link, err)
			return err
		}
		return nil
	}
	links, err := processPage(ctx, link)
	if err != nil {
		fmt.Printf("[ERROR] Failed to process page %s: %v\n", link, err)
		return err
	}
	fmt.Printf("[INFO] Processed page: %s, found %d new links\n", link, len(links))
	for _, l := range links {
		if err := enqueueLink(ctx, l, tasks); err != nil {
			// loggin error and breaking processing in case of full channel
			fmt.Printf("[ERROR] %v\n", err)
			return err
		}
	}
	return nil
}

// processPage downloads the HTML page, filters unwanted tags, modifies links, saves it, and returns newly found links
func processPage(ctx context.Context, pageURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download page failed: %s, HTTP status: %d, %s", pageURL, resp.StatusCode, resp.Status)
	}

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

// filterDocument removes unwanted HTML tags based on predefined rules
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

// modifyLinks processes href/src in HTML, calls fixPath to decode/remove diacritics, then sets relative URLs
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
						// decode percent-encoded path, remove diacritics, etc.
						fixPath(absLink)

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
			// Additional handling for <script>...</script>
			if node.Data == "script" {
				var scriptID string
				for _, a := range node.Attr {
					if a.Key == "id" {
						scriptID = a.Val
						break
					}
				}
				// only process specific scripts, here we process Simple Lightbox scripts
				if scriptID == "slb_footer" || scriptID == "slb_context" {
					jsContent := getTextContent(node)
					newJS, jsLinks := processInlineJS(jsContent, currentURL, baseURL)
					replaceTextContent(node, newJS)
					foundLinks = append(foundLinks, jsLinks...)
				}
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
			// decode path and remove diacritics
			fixPath(absLink)

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

// sanitizeQueryPart replaces special characters in query
func sanitizeQueryPart(s string) string {
	s = strings.ReplaceAll(s, "=", "_")
	s = strings.ReplaceAll(s, "?", "_")
	s = strings.ReplaceAll(s, "&", "_")
	return s
}

// unifyScheme ensures the scheme matches if host is the same
func unifyScheme(u, base *url.URL) {
	if u.Host == base.Host {
		u.Scheme = base.Scheme
	}
}

// resolveURL returns absolute URL from currentURL + link
func resolveURL(currentURL, link string) (*url.URL, error) {
	base, err := url.Parse(currentURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}

	abs := base.ResolveReference(u)
	unifyScheme(abs, base) // unify the scheme (http -> https) if the host is the same
	return abs, nil
}

// processInlineStyle handles style="... url(...) ..."
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
			fixPath(absLink)

			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})
	return newStyle, foundLinks
}

// processInlineCSS handles <style>...</style> content with url(...)
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
			fixPath(absLink)

			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})
	return newCSS, foundLinks
}

// processInlineJS searches within the JS content (e.g., in the Simple Lightbox code)
// for links of type http:// ... or https:// ... (often escaped as http:\/\/...),
// rewrites them, and returns a list of newly found links so they can be downloaded.
func processInlineJS(jsContent, currentURL, baseURL string) (string, []string) {
	var foundLinks []string

	// First, "un-escape" the sequence "\/" to "/",
	// so that a regular expression or link parser can correctly recognize them.
	unescaped := strings.ReplaceAll(jsContent, `\/`, `/`)

	// decode unicode escapes \uXXXX np. \u2013 -> – (en dash)
	unescaped = decodeUnicodeEscapes(unescaped)

	newJS := reJS.ReplaceAllStringFunc(unescaped, func(match string) string {
		// match = e.g., "http://olamundo.pl/wp-content/uploads/2014/11/dante-gabriel-rossetti.jpg"
		absLink, err := resolveURL(currentURL, match)
		if err != nil {
			return match // Do not change if it cannot be parsed
		}

		// Check if it is from the same domain
		if isSameDomain(absLink.String(), baseURL) {
			foundLinks = append(foundLinks, absLink.String())

			// Rewrite if -rewriteUrl is enabled
			if *rewriteUrl && absLink.RawQuery != "" {
				if isStaticAsset(absLink.String()) {
					absLink = rewriteAssetURL(absLink)
				} else {
					absLink = rewritePageURL(absLink)
				}
			}
			fixPath(absLink)

			// Replace the absolute link with a relative one (like in CSS)
			relative := convertToRelative(absLink.String(), baseURL)

			// Restore escape sequences like "\/" instead of "/"
			// NOTE: we must replace *only* "/" with "\/" to keep the script correct
			escaped := strings.ReplaceAll(relative, "/", `\/`)

			// Return the escaped, replaced version
			return escaped
		}

		// If different domain, leave the original
		return match
	})

	return newJS, foundLinks
}

// processCSSFile processes the content of a CSS file (downloaded from the server):
func processCSSFile(cssContent string, cssURL, baseURL string) (string, []string) {
	var foundLinks []string

	// similar to processInlineCSS
	newCSS := re.ReplaceAllStringFunc(cssContent, func(match string) string {
		urls := re.FindStringSubmatch(match)
		if len(urls) < 2 {
			return match
		}
		originalLink := urls[1]

		// if starts with data:, do nothing and return the original
		// data is base64 encoded image, we don't want to process it
		if strings.HasPrefix(originalLink, "data:") {
			return match
		}

		absLink, err := resolveURL(cssURL, originalLink)
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
			fixPath(absLink)
			relativeURL := convertToRelative(absLink.String(), baseURL)
			return fmt.Sprintf("url('%s')", relativeURL)
		}
		return match
	})

	return newCSS, foundLinks
}

// convertToRelative removes the base from the link if it starts with it
func convertToRelative(link, base string) string {
	if strings.HasPrefix(link, base) {
		return strings.TrimPrefix(link, base)
	}
	return link
}

// isSameDomain checks if link and base share the same host
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

// downloadAsset downloads the asset from the server and saves it to the disk.
// If the asset is a CSS file, it will be processed to rewrite URLs.
func downloadAsset(ctx context.Context, link string, tasks chan<- string) error {
	outputFile := getOutputPath(link)

	// Check if the file already exists
	if _, err := os.Stat(outputFile); err == nil {
		//fmt.Printf("[INFO] Asset already exists: %s\n", outputFile)
		return nil
	}

	//fmt.Printf("[INFO] Downloading asset: %s\n", link)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download asset failed, HTTP status: %d", resp.StatusCode)
	}

	err = os.MkdirAll(filepath.Dir(outputFile), 0755)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(link))

	// if the file is CSS, we will first download it to memory, process it, and then save it to disk
	if ext == ".css" {
		cssBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		cssContent := string(cssBytes)

		// processing CSS file to rewrite url(...)
		newCSS, foundLinks := processCSSFile(cssContent, link, *baseURL)

		// every new link from the same domain, enqueue:
		for _, l := range foundLinks {
			enqueueLink(ctx, l, tasks)
		}

		// save the modified CSS content to the file
		file, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = file.WriteString(newCSS)
		if err == nil {
			fmt.Printf("[INFO] Saved CSS asset: %s -> %s\n", link, outputFile)
		}
		return err
	}

	// in other cases (images, fonts, js, etc.) we copy directly:
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

// getOutputPath determines how the file is saved locally
func getOutputPath(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(*outputDir, "index.html")
	}

	path := norm.NFC.String(u.Path)
	path, err = removeDiacritics(path)
	if err != nil {
		path = norm.NFC.String(u.Path)
	}

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

	finalPath := filepath.Join(*outputDir, path)
	finalPath = norm.NFC.String(finalPath)
	finalPath, err = removeDiacritics(finalPath)
	if err != nil {
		finalPath = norm.NFC.String(finalPath) // Fallback
	}
	return finalPath
}

// saveHTML writes the HTML document to a file
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

// decodeUnicodeEscapes turns \uXXXX sequences into real Unicode runes
func decodeUnicodeEscapes(s string) string {
	return unicodeEsc.ReplaceAllStringFunc(s, func(m string) string {
		// m has the form e.g. "\u2013"
		hexVal := m[2:] // remove "\u" prefix
		r, err := strconv.ParseInt(hexVal, 16, 32)
		if err != nil {
			return m
		}
		return string(rune(r))
	})
}

// removeDiacritics removes combining marks from a string and normalizes to NFC
func removeDiacritics(s string) (string, error) {
	t := transform.Chain(
		norm.NFD,                           // Normalize to NFD (decomposed form)
		runes.Remove(runes.In(unicode.Mn)), // Remove all non-spacing marks
		norm.NFC,                           // Recompose to NFC (composed form)
	)
	result, _, err := transform.String(t, s)
	return result, err
}

// fixPath unescapes the path, removes diacritics, re-normalizes to NFC, then updates the URL
func fixPath(u *url.URL) {
	unescaped, err := url.PathUnescape(u.EscapedPath())
	if err == nil {
		cleaned, _ := removeDiacritics(unescaped)
		cleaned = norm.NFC.String(cleaned)
		u.Path = cleaned
		// It's good practice to reset RawPath
		u.RawPath = ""
	}
}
