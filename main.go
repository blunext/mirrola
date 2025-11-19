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
)

// --- Config & globals ---

var (
	concurrency int
	queueSize   int

	baseURL    *string
	outputDir  *string
	rewriteURL *bool
	userAgent  *string
	timeoutSec *int

	client *http.Client

	visited = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}

	reCSSURL   = regexp.MustCompile(`url\(\s*['"]?\s*([^'\")]+?)\s*['"]?\s*\)`) // url('...')
	reJSAbsURL = regexp.MustCompile(`(https?://[^\s"']+)`)                      // https://...
	unicodeEsc = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`)

	tasksWg sync.WaitGroup
)

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
	flag.IntVar(&queueSize, "queue", 10000, "Task queue size")
	flag.IntVar(&concurrency, "concurrency", runtime.NumCPU(), "Number of workers")
	flag.Parse()

	if *baseURL == "" {
		fmt.Println("Error: -url is required")
		os.Exit(2)
	}

	client = &http.Client{Timeout: time.Duration(*timeoutSec) * time.Second, Transport: &http.Transport{MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Starting download for %s, workers: %d\n", *baseURL, concurrency)
	tasks := make(chan string, queueSize)

	if err := enqueueLink(ctx, *baseURL, tasks); err != nil {
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
				case link, ok := <-tasks:
					if !ok {
						return
					}
					func() {
						defer tasksWg.Done()
						if err := processURL(ctx, link, tasks); err != nil {
							fmt.Printf("[ERROR] %s: %v\n", link, err)
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
	// clean dot segments
	if p := u.EscapedPath(); p != "" {
		u.Path = path.Clean("/" + p)
	}
	return u
}

func enqueueLink(ctx context.Context, link string, tasks chan<- string) error {
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}
	if disallowedSchemes[strings.ToLower(u.Scheme)] {
		return nil
	}
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
	case tasks <- link:
		return nil
	}
}

// --- HTTP helpers ---

func doRequest(ctx context.Context, method, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", *userAgent)
	return client.Do(req)
}

func headOrGet(ctx context.Context, u string) (*http.Response, error) {
	// Try HEAD first to check content type and availability
	resp, err := doRequest(ctx, http.MethodHead, u)
	if err != nil || resp.StatusCode >= 400 {
		// HEAD failed, try GET directly
		if resp != nil {
			resp.Body.Close()
		}
		return doRequest(ctx, http.MethodGet, u)
	}
	// HEAD succeeded, close it and do GET to get the body
	resp.Body.Close()
	return doRequest(ctx, http.MethodGet, u)
}

// --- Routing based on Content-Type ---

func processURL(ctx context.Context, link string, tasks chan<- string) error {
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

	switch {
	case ct == "text/html" || ct == "application/xhtml+xml" || (ct == "" && looksLikeHTML(resp)):
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		links, err := processHTML(ctx, link, body)
		if err != nil {
			return err
		}
		for _, l := range links {
			_ = enqueueLink(ctx, l, tasks)
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
		for _, l := range found {
			_ = enqueueLink(ctx, l, tasks)
		}
		out := getOutputPath(link, ct)
		return writeFile(out, strings.NewReader(newCSS))
	default:
		// binary or other asset
		out := getOutputPath(link, ct)
		return streamToFile(out, resp.Body)
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

func processHTML(ctx context.Context, pageURL string, body []byte) ([]string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// compute base href, if present
	currentBase := pageURL
	if b := findBaseHref(doc); b != "" {
		if abs, err := resolveURL(pageURL, b); err == nil {
			currentBase = abs.String()
		}
	}

	filterDocument(doc)
	pageLinks := rewriteLinks(doc, currentBase, *baseURL)

	outputFile := getOutputPath(pageURL, "text/html")
	if err := saveHTML(outputFile, doc); err != nil {
		return nil, err
	}
	fmt.Printf("[INFO] Saved page: %s -> %s\n", pageURL, outputFile)
	return pageLinks, nil
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
		}
		for c := node.FirstChild; c != nil; {
			next := c.NextSibling
			f(c)
			c = next
		}
	}
	f(n)
}

func rewriteLinks(n *html.Node, currentURL, base string) []string {
	var found []string
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for i, attr := range node.Attr {
				switch strings.ToLower(attr.Key) {
				case "href", "src":
					orig := attr.Val
					abs, err := resolveURL(currentURL, orig)
					if err == nil && sameHost(abs.String(), base) {
						found = append(found, abs.String())
						if *rewriteURL && abs.RawQuery != "" {
							abs = rewriteURLWithPolicy(abs)
						}
						fixPath(abs)
						rel := toRelative(abs, base)
						node.Attr[i].Val = rel
					}
				case "style":
					newStyle, links := processInlineStyle(attr.Val, currentURL, base)
					node.Attr[i].Val = newStyle
					found = append(found, links...)
				case "srcset":
					newSrc, links := processSrcSet(attr.Val, currentURL, base)
					node.Attr[i].Val = newSrc
					found = append(found, links...)
				}
			}

			// <style>...</style>
			if strings.EqualFold(node.Data, "style") {
				css := getTextContent(node)
				newCSS, links := processInlineCSS(css, currentURL, base)
				replaceTextContent(node, newCSS)
				found = append(found, links...)
			}

			// <source srcset> in <picture>, <video>/<audio> sources
			if strings.EqualFold(node.Data, "source") {
				for i, a := range node.Attr {
					if strings.EqualFold(a.Key, "srcset") {
						newSrc, links := processSrcSet(a.Val, currentURL, base)
						node.Attr[i].Val = newSrc
						found = append(found, links...)
					}
					if strings.EqualFold(a.Key, "src") {
						abs, err := resolveURL(currentURL, a.Val)
						if err == nil && sameHost(abs.String(), base) {
							found = append(found, abs.String())
							if *rewriteURL && abs.RawQuery != "" {
								abs = rewriteURLWithPolicy(abs)
							}
							fixPath(abs)
							node.Attr[i].Val = toRelative(abs, base)
						}
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return found
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

// Optional: generic inline JS URL rewriter (kept conservative)
func processInlineJS(jsContent, currentURL, base string) (string, []string) {
	var found []string
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
	return abs.Path + func() string {
		if abs.RawQuery != "" {
			return "?" + abs.RawQuery
		}
		return ""
	}()
}

// --- Asset I/O ---

func getOutputPath(link string, contentType string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(*outputDir, "index.html")
	}

	pathPart := norm.NFC.String(u.Path)
	if cleaned, err2 := removeDiacritics(pathPart); err2 == nil {
		pathPart = cleaned
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
	}

	final := filepath.Join(*outputDir, pathPart)
	final = norm.NFC.String(final)
	if cleaned, err2 := removeDiacritics(final); err2 == nil {
		final = cleaned
	}
	return final
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

func streamToFile(path string, r io.Reader) error { return writeFile(path, r) }

func saveHTML(outputFile string, doc *html.Node) error {
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return err
	}
	return writeFile(outputFile, &buf)
}

// --- Path cleanup ---

func isStaticAssetExt(ext string) bool {
	ext = strings.ToLower(ext)
	staticExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".ico", ".css", ".js", ".woff", ".woff2", ".ttf", ".eot", ".otf", ".pdf"}
	for _, e := range staticExts {
		if ext == e {
			return true
		}
	}
	return false
}

func removeDiacritics(s string) (string, error) {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	return result, err
}

func fixPath(u *url.URL) {
	unescaped, err := url.PathUnescape(u.EscapedPath())
	if err == nil {
		if cleaned, _ := removeDiacritics(unescaped); cleaned != "" {
			u.Path = norm.NFC.String(cleaned)
		}
		u.RawPath = ""
	}
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
		hexVal := m[2:]
		r, err := strconv.ParseInt(hexVal, 16, 32)
		if err != nil {
			return m
		}
		return string(rune(r))
	})
}

// --- END ---
