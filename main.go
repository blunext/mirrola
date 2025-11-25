package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/time/rate"
)

// --- Config & globals ---

var (
	concurrency int
	queueSize   int
	maxDepth    int

	baseURL           *string
	outputDir         *string
	rewriteURL        *bool
	safeFilenames     *bool
	userAgent         *string
	timeoutSec        *int
	requestsPerSecond *uint

	client  *http.Client
	limiter *rate.Limiter

	visited = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}

	reCSSURL   *regexp.Regexp
	reJSAbsURL *regexp.Regexp
	unicodeEsc *regexp.Regexp

	tasksWg sync.WaitGroup
)

func init() {
	reCSSURL = regexp.MustCompile(`url\(\s*['"]?\s*([^'\")]+?)\s*['"]?\s*\)`) // url('...')
	reJSAbsURL = regexp.MustCompile(`(https?://[^\s"']+)`)                    // https://...
	unicodeEsc = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`)
}

// task represents a URL to crawl with its depth
type task struct {
	url   string
	depth int
}

// Unwanted tags to strip (mostly WP-specific)
type unwantedTag struct {
	Tag   string
	Attrs map[string]string
}

var unwantedTags = []unwantedTag{
	{Tag: "link", Attrs: map[string]string{"rel": "shortlink"}},
	{Tag: "link", Attrs: map[string]string{"rel": "pingback"}},
	{Tag: "link", Attrs: map[string]string{"rel": "EditURI"}},
	{Tag: "link", Attrs: map[string]string{"rel": "https://api.w.org/"}},
	{Tag: "link", Attrs: map[string]string{"rel": "alternate", "title": "JSON", "type": "application/json"}},
	{Tag: "link", Attrs: map[string]string{"rel": "alternate", "type": "application/json+oembed"}},
	{Tag: "link", Attrs: map[string]string{"rel": "alternate", "type": "text/xml+oembed"}},
}

// Schemes we never enqueue
var disallowedSchemes = map[string]bool{"mailto": true, "tel": true, "javascript": true, "data": true}

func main() {
	// Flags
	baseURL = flag.String("url", "", "Base URL to start crawling (required)")
	outputDir = flag.String("dir", "./static", "Output directory")
	rewriteURL = flag.Bool("rewrite", false, "Bake query params into filenames")
	userAgent = flag.String("ua", "StaticCrawler/1.0", "HTTP User-Agent")
	timeoutSec = flag.Int("timeout", 20, "HTTP timeout in seconds")
	requestsPerSecond = flag.Uint("rate", 0, "Max requests per second (0 = unlimited)")
	flag.IntVar(&queueSize, "queue", 10000, "Task queue size")
	flag.IntVar(&concurrency, "concurrency", runtime.NumCPU(), "Number of workers")
	flag.IntVar(&maxDepth, "max-depth", 0, "Maximum crawl depth (0 = unlimited, 1 = current page only, 2 = current + links, etc.)")
	safeFilenames = flag.Bool("safe-filenames", false, "Use percent-encoded filenames (safer) instead of ASCII transliteration")
	flag.Parse()

	if *baseURL == "" {
		flag.Usage()
		os.Exit(1)
	}

	client = &http.Client{Timeout: time.Duration(*timeoutSec) * time.Second, Transport: &http.Transport{MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second}}

	// Initialize rate limiter if rate limiting is enabled
	if *requestsPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(float64(*requestsPerSecond)), 1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Starting download for %s, workers: %d\n", *baseURL, concurrency)
	if maxDepth > 0 {
		fmt.Printf("Max depth: %d\n", maxDepth)
	}
	if *requestsPerSecond > 0 {
		fmt.Printf("Rate limit: %d requests/second\n", *requestsPerSecond)
	}
	tasks := make(chan task, queueSize)

	if err := enqueueLink(ctx, *baseURL, 0, tasks); err != nil {
		fmt.Println("enqueue error:", err)
		os.Exit(1)
	}

	var processError atomic.Value // first error wins

	go func() {
		tasksWg.Wait()
		cancel()
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
				case t, ok := <-tasks:
					if !ok {
						return
					}
					func() {
						defer tasksWg.Done()
						if err := processURL(ctx, t.url, t.depth, tasks); err != nil {
							fmt.Printf("[ERROR] %s: %v\n", t.url, err)
							if processError.Load() == nil {
								processError.Store(err)
								cancel()
							}
						}
					}()
				}
			}
		}()
	}

	workersWg.Wait()
	if err, ok := processError.Load().(error); ok && err != nil {
		fmt.Printf("[ERROR] Processing failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Downloading completed.")
}

// --- Queueing & normalization ---

func normalize(u *url.URL) *url.URL {
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	// remove default ports
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) || (u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = strings.Split(u.Host, ":")[0]
	}
	// clean dot segments using decoded path
	if u.Path != "" {
		// path.Clean assumes forward slashes which is correct for URLs
		cleaned := path.Clean(u.Path)
		// Ensure absolute path starts with / if original did (path.Clean might remove it if it thinks it's relative?)
		// Actually path.Clean("/") -> "/". path.Clean("/foo") -> "/foo".
		// But path.Clean("foo") -> "foo".
		// URL path usually starts with / if it's absolute path.
		if strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		u.Path = cleaned
	}
	u.RawPath = "" // Clear RawPath to force re-encoding based on new Path
	return u
}

func enqueueLink(ctx context.Context, link string, depth int, tasks chan<- task) error {
	// Check max depth (0 = unlimited, 1 = current page only, etc.)
	if maxDepth > 0 && depth >= maxDepth {
		return nil // Skip URLs at or beyond max depth
	}

	u, err := url.Parse(link)
	if err != nil {
		return fmt.Errorf("failed to parse URL %s: %w", link, err)
	}
	if disallowedSchemes[strings.ToLower(u.Scheme)] {
		return nil
	}
	fixPath(u)
	u = normalize(u)
	link = u.String()

	visited.Lock()
	if visited.m[link] {
		visited.Unlock()
		return nil
	}
	visited.m[link] = true
	visited.Unlock()

	tasksWg.Add(1)
	select {
	case <-ctx.Done():
		tasksWg.Done()
		return ctx.Err()
	case tasks <- task{url: link, depth: depth}:
		return nil
	}
}

// --- HTTP helpers ---

func doRequest(ctx context.Context, method, u string) (*http.Response, error) {
	// Wait for rate limiter if enabled
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", *userAgent)
	return client.Do(req)
}

func headOrGet(ctx context.Context, u string) (*http.Response, error) {
	// Helper to try HEAD then GET
	try := func(targetURL string) (*http.Response, error) {
		// Try HEAD first
		resp, err := doRequest(ctx, http.MethodHead, targetURL)
		if err != nil || resp.StatusCode >= 400 {
			// HEAD failed or error status, try GET directly
			if resp != nil {
				resp.Body.Close()
			}
			return doRequest(ctx, http.MethodGet, targetURL)
		}
		// HEAD succeeded, close it and do GET to get the body
		resp.Body.Close()
		return doRequest(ctx, http.MethodGet, targetURL)
	}

	resp, err := try(u)

	// If 404, try switching normalization (NFC <-> NFD)
	if err == nil && resp.StatusCode == 404 {
		resp.Body.Close() // Close the 404 response

		parsed, parseErr := url.Parse(u)
		if parseErr == nil {
			path := parsed.Path
			var altPath string

			// Check if we can flip normalization
			if norm.NFC.IsNormalString(path) {
				altPath = norm.NFD.String(path)
			} else {
				altPath = norm.NFC.String(path)
			}

			if altPath != path {
				parsed.Path = altPath
				parsed.RawPath = "" // Force re-encoding
				altURL := parsed.String()

				// fmt.Printf("[INFO] 404 for %s, trying fallback: %s\n", u, altURL)
				resp2, err2 := try(altURL)
				if err2 == nil && resp2.StatusCode < 400 {
					return resp2, nil // Found it!
				}
				if resp2 != nil {
					resp2.Body.Close()
				}
			}
		}
		// If fallback failed, return the original 404 response (we need to re-request it or just return error?
		// Actually, we closed the body. Let's just return a new error or re-request.
		// Re-requesting is safer to return a valid response object.
		return try(u)
	}

	return resp, err
}

// --- Routing based on Content-Type ---

func processURL(ctx context.Context, link string, depth int, tasks chan<- task) error {
	resp, err := headOrGet(ctx, link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}

	isHTML := ct == "text/html" || ct == "application/xhtml+xml"
	if !isHTML && ct == "" {
		if looksLikeHTML(resp) {
			isHTML = true
		}
	}

	switch {
	case isHTML:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		assets, links, err := processHTML(ctx, link, body)
		if err != nil {
			return err
		}
		// Assets inherit current depth (they are part of the page)
		for _, a := range assets {
			if err := enqueueLink(ctx, a, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue asset %s: %v\n", a, err)
			}
		}
		// Links increase depth
		for _, l := range links {
			if err := enqueueLink(ctx, l, depth+1, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue link %s: %v\n", l, err)
			}
		}
		return nil
	case ct == "text/css":
		// treat as CSS asset with rewriting
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		css := string(b)
		newCSS, found := processCSSFile(css, link, *baseURL)
		// CSS assets also inherit current depth
		for _, l := range found {
			if err := enqueueLink(ctx, l, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue CSS link %s: %v\n", l, err)
			}
		}
		out := getOutputPath(link, ct)
		return writeFile(out, strings.NewReader(newCSS))
	default:
		// binary or other asset
		out := getOutputPath(link, ct)
		return writeFile(out, resp.Body)
	}
}

func looksLikeHTML(resp *http.Response) bool {
	buf := make([]byte, 256)
	n, _ := io.ReadFull(resp.Body, buf)
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf[:n]), resp.Body))
	b := strings.ToLower(string(buf[:n]))
	return strings.Contains(b, "<html") || strings.Contains(b, "<!doctype")
}

// --- HTML processing ---

func processHTML(ctx context.Context, pageURL string, body []byte) ([]string, []string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}

	// compute base href, if present
	currentBase := pageURL
	if b := findBaseHref(doc); b != "" {
		if abs, err := resolveURL(pageURL, b); err == nil {
			currentBase = abs.String()
		}
	}

	filterDocument(doc)
	assets, links := rewriteLinks(doc, currentBase, *baseURL)

	outputFile := getOutputPath(pageURL, "text/html")
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
	// Check for comment-reply.min.js in src attribute
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "src") && strings.Contains(attr.Val, "comment-reply.min.js") {
			return true
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
	isAsset := false
	if strings.EqualFold(node.Data, "link") {
		// check rel
		for _, a := range node.Attr {
			if strings.EqualFold(a.Key, "rel") {
				val := strings.ToLower(a.Val)
				if strings.Contains(val, "stylesheet") || strings.Contains(val, "icon") {
					isAsset = true
				}
				break
			}
		}
	}

	orig := attr.Val
	abs, err := resolveURL(currentURL, orig)
	if err == nil && sameHost(abs.String(), base) {
		if isAsset {
			assets = append(assets, abs.String())
		} else {
			links = append(links, abs.String())
		}
		if *rewriteURL && abs.RawQuery != "" {
			abs = rewriteURLWithPolicy(abs)
		}
		fixPath(abs)
		attr.Val = toRelative(abs, base)
	}
	return assets, links
}

// processSrcAttribute handles src attributes (always assets)
func processSrcAttribute(attr *html.Attribute, currentURL, base string) []string {
	var assets []string

	orig := attr.Val
	abs, err := resolveURL(currentURL, orig)
	if err == nil && sameHost(abs.String(), base) {
		assets = append(assets, abs.String())
		if *rewriteURL && abs.RawQuery != "" {
			abs = rewriteURLWithPolicy(abs)
		}
		fixPath(abs)
		attr.Val = toRelative(abs, base)
	}
	return assets
}

// processStyleAttribute handles inline style attributes
func processStyleAttribute(attr *html.Attribute, currentURL, base string) []string {
	newStyle, found := processInlineStyle(attr.Val, currentURL, base)
	attr.Val = newStyle
	return found
}

// processSrcsetAttribute handles srcset attributes
func processSrcsetAttribute(attr *html.Attribute, currentURL, base string) []string {
	newSrc, found := processSrcSet(attr.Val, currentURL, base)
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
	case "source":
		assets = append(assets, processSourceElement(node, currentURL, base)...)
	}
	return assets
}

// processStyleElement handles <style>...</style> elements
func processStyleElement(node *html.Node, currentURL, base string) []string {
	css := getTextContent(node)
	newCSS, found := processInlineCSS(css, currentURL, base)
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
		newJS, found := processInlineJS(jsContent, currentURL, base)
		replaceTextContent(node, newJS)
		return found
	}
	return nil
}

// processSourceElement handles <source> elements in <picture>, <video>/<audio>
func processSourceElement(node *html.Node, currentURL, base string) []string {
	var assets []string

	for i := range node.Attr {
		attr := &node.Attr[i]
		if strings.EqualFold(attr.Key, "srcset") {
			newSrc, found := processSrcSet(attr.Val, currentURL, base)
			attr.Val = newSrc
			assets = append(assets, found...)
		}
		if strings.EqualFold(attr.Key, "src") {
			abs, err := resolveURL(currentURL, attr.Val)
			if err == nil && sameHost(abs.String(), base) {
				assets = append(assets, abs.String())
				if *rewriteURL && abs.RawQuery != "" {
					abs = rewriteURLWithPolicy(abs)
				}
				fixPath(abs)
				attr.Val = toRelative(abs, base)
			}
		}
	}
	return assets
}

// --- Attribute processors ---

func processSrcSet(srcset string, currentURL, base string) (string, []string) {
	var found, partsOut []string
	for _, part := range strings.Split(srcset, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		img := fields[0]
		desc := ""
		if len(fields) > 1 {
			desc = fields[1]
		}
		abs, err := resolveURL(currentURL, img)
		if err != nil {
			partsOut = append(partsOut, part)
			continue
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			rel := toRelative(abs, base)
			if desc != "" {
				partsOut = append(partsOut, rel+" "+desc)
			} else {
				partsOut = append(partsOut, rel)
			}
		} else {
			partsOut = append(partsOut, part)
		}
	}
	return strings.Join(partsOut, ", "), found
}

func processInlineStyle(style, currentURL, base string) (string, []string) {
	found := []string{}
	out := reCSSURL.ReplaceAllStringFunc(style, func(m string) string {
		urls := reCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		abs, err := resolveURL(currentURL, urls[1])
		if err != nil {
			return m
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", toRelative(abs, base))
		}
		return m
	})
	return out, found
}

func processInlineCSS(css, currentURL, base string) (string, []string) {
	found := []string{}
	out := reCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := reCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		orig := urls[1]
		if strings.HasPrefix(orig, "data:") {
			return m
		}
		abs, err := resolveURL(currentURL, orig)
		if err != nil {
			return m
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", toRelative(abs, base))
		}
		return m
	})
	return out, found
}

// processInlineJS processes inline JavaScript for Simple Lightbox plugin
// It finds HTTP(S) URLs, rewrites them to relative paths, and collects assets
func processInlineJS(jsContent, currentURL, base string) (string, []string) {
	var found []string
	// Unescape \/ to / and decode unicode escapes like \u0026
	unescaped := strings.ReplaceAll(jsContent, `\/`, `/`)
	unescaped = decodeUnicodeEscapes(unescaped)
	newJS := reJSAbsURL.ReplaceAllStringFunc(unescaped, func(match string) string {
		abs, err := resolveURL(currentURL, match)
		if err != nil {
			return match
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			rel := toRelative(abs, base)
			// Re-escape slashes for JS strings
			return strings.ReplaceAll(rel, "/", `\/`)
		}
		return match
	})
	return newJS, found
}

// processCSSFile processes CSS file content and rewrites URLs
func processCSSFile(css, cssURL, base string) (string, []string) {
	var found []string
	out := reCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := reCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		orig := urls[1]
		if strings.HasPrefix(orig, "data:") {
			return m
		}
		abs, err := resolveURL(cssURL, orig)
		if err != nil {
			return m
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", toRelative(abs, base))
		}
		return m
	})
	return out, found
}

// --- URL helpers ---

func sameHost(link, base string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	b, err := url.Parse(base)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, b.Host)
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
	abs := base.ResolveReference(u)
	// unify scheme for same host
	if strings.EqualFold(abs.Host, base.Host) {
		abs.Scheme = base.Scheme
	}
	return normalize(abs), nil
}

func toRelative(abs *url.URL, base string) string {
	b, err := url.Parse(base)
	if err != nil {
		return abs.String()
	}
	if !strings.EqualFold(abs.Host, b.Host) {
		return abs.String()
	}

	// If safe filenames are enabled, use EscapedPath to ensure links in HTML match the files on disk
	// and to prevent html.Render from normalizing UTF-8 characters to NFD.
	var pathStr string
	if safeFilenames != nil && *safeFilenames {
		// Force NFC for local links because getOutputPath saves files as NFC.
		// We need the link in HTML (%C3%B3) to match the file on disk (ó).
		// If we used abs.EscapedPath() directly, it might be NFD (%CC%81) if the source was NFD,
		// which would mismatch the NFC file on non-Mac filesystems.
		pathStr = (&url.URL{Path: norm.NFC.String(abs.Path)}).EscapedPath()
	} else {
		pathStr = abs.Path
	}

	if abs.RawQuery != "" {
		return pathStr + "?" + abs.RawQuery
	}
	return pathStr
}

// --- Asset I/O ---

func getOutputPath(link string, contentType string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(*outputDir, "index.html")
	}

	// Start with NFC-normalized path
	pathPart := norm.NFC.String(u.Path)

	// Apply transliteration if not using safe filenames
	if safeFilenames == nil || !*safeFilenames {
		if cleaned, err2 := removeDiacritics(pathPart); err2 == nil {
			pathPart = cleaned
		}
	}

	if pathPart == "" || pathPart == "/" {
		pathPart = "/index.html"
	} else {
		ext := filepath.Ext(pathPart)
		if ext == "" {
			// Decide based on content-type
			switch contentType {
			case "text/html", "application/xhtml+xml":
				if strings.HasSuffix(pathPart, "/") {
					pathPart += "index.html"
				} else {
					pathPart = pathPart + "/index.html"
				}
			case "text/css":
				pathPart += ".css"
			case "application/javascript", "text/javascript":
				pathPart += ".js"
			default:
				if ext2, _ := extForContentType(contentType); ext2 != "" {
					pathPart += ext2
				}
			}
		}
	}

	// Query baking
	if *rewriteURL && u.RawQuery != "" {
		if isStaticAssetExt(filepath.Ext(pathPart)) {
			u = rewriteAssetURL(u)
		} else {
			u = rewritePageURL(u)
		}
		pathPart = u.Path
		// Re-apply transliteration after query baking if needed
		if safeFilenames == nil || !*safeFilenames {
			if cleaned, err2 := removeDiacritics(pathPart); err2 == nil {
				pathPart = cleaned
			}
		}
	}

	return filepath.Join(*outputDir, pathPart)
}

func extForContentType(ct string) (string, bool) {
	switch ct {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/svg+xml":
		return ".svg", true
	case "font/woff":
		return ".woff", true
	case "font/woff2":
		return ".woff2", true
	case "application/json":
		return ".json", true
	case "application/pdf":
		return ".pdf", true
	}
	if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
		return exts[0], true
	}
	return "", false
}

func writeFile(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveHTML(outputFile string, doc *html.Node) error {
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return err
	}
	return writeFile(outputFile, &buf)
}

// --- Path cleanup ---

var staticAssetExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".svg": true, ".ico": true, ".css": true, ".js": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true, ".pdf": true,
}

func isStaticAssetExt(ext string) bool {
	return staticAssetExts[strings.ToLower(ext)]
}

func removeDiacritics(s string) (string, error) {
	// First, handle Polish special characters that don't decompose with NFD
	replacer := strings.NewReplacer(
		"ł", "l", "Ł", "L",
		"ą", "a", "Ą", "A",
		"ć", "c", "Ć", "C",
		"ę", "e", "Ę", "E",
		"ń", "n", "Ń", "N",
		"ś", "s", "Ś", "S",
		"ź", "z", "Ź", "Z",
		"ż", "z", "Ż", "Z",
	)
	s = replacer.Replace(s)

	// Then apply NFD normalization for composed diacritics (ó, á, etc.)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	return result, err
}

func fixPath(u *url.URL) {
	// Clear RawPath to force re-encoding based on Path
	u.RawPath = ""
}

// --- Query rewrite policy (with hashing for long queries) ---

func rewriteURLWithPolicy(u *url.URL) *url.URL {
	if len(u.RawQuery) > 80 { // hash long queries
		sum := sha1.Sum([]byte(u.RawQuery))
		sfx := hex.EncodeToString(sum[:8])
		if filepath.Ext(u.Path) == "" { // page-like
			return rewritePageURLWithSuffix(u, "q_"+sfx)
		}
		return rewriteAssetURLWithSuffix(u, "q_"+sfx)
	}
	// short queries → readable
	if filepath.Ext(u.Path) == "" {
		return rewritePageURL(u)
	}
	return rewriteAssetURL(u)
}

func sanitizeQueryPart(s string) string {
	s = strings.ReplaceAll(s, "=", "_")
	s = strings.ReplaceAll(s, "?", "_")
	s = strings.ReplaceAll(s, "&", "_")
	return s
}

func rewriteAssetURL(u *url.URL) *url.URL { return rewriteAssetURLWithSuffix(u, querySuffix(u)) }

func querySuffix(u *url.URL) string {
	q := u.Query()
	if len(q) == 0 {
		return ""
	}
	parts := make([]string, 0, len(q))
	for k, vals := range q {
		for _, v := range vals {
			parts = append(parts, sanitizeQueryPart(k)+"_"+sanitizeQueryPart(v))
		}
	}
	return strings.Join(parts, "_")
}

func rewriteAssetURLWithSuffix(u *url.URL, suffix string) *url.URL {
	if u.RawQuery == "" {
		return u
	}
	ext := filepath.Ext(u.Path)
	if ext == "" {
		u.RawQuery = ""
		return u
	}
	base := strings.TrimSuffix(u.Path, ext)
	if suffix != "" {
		u.Path = base + "_" + suffix + ext
	} else {
		u.Path = base + ext
	}
	u.RawQuery = ""
	return u
}

func rewritePageURL(u *url.URL) *url.URL {
	return rewritePageURLWithSuffix(u, sanitizeQueryPart(u.RawQuery))
}

func rewritePageURLWithSuffix(u *url.URL, suffix string) *url.URL {
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
	d := filepath.Dir(u.Path)
	b := filepath.Base(u.Path)
	name := strings.TrimSuffix(b, ext)
	if suffix != "" {
		b = name + "_" + suffix + ext
	} else {
		b = name + ext
	}
	u.Path = filepath.Join(d, b)
	u.RawQuery = ""
	return u
}

// --- Text helpers ---

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

func decodeUnicodeEscapes(s string) string {
	return unicodeEsc.ReplaceAllStringFunc(s, func(m string) string {
		hexVal := m[2:] // Skip \u prefix
		r, err := strconv.ParseInt(hexVal, 16, 32)
		if err != nil {
			return m
		}
		return string(rune(r))
	})
}

// --- END ---
