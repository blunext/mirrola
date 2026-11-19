package main

import (
	"bytes"
	"crypto/sha256"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// file is what sitecheck knows about one file of a mirror.
type file struct {
	hash  [32]byte // of the normalized content for HTML/CSS/JS, raw otherwise
	page  bool     // HTML
	text  []string // visible text lines (pages only)
	links []string // root-relative links found in it (HTML and CSS)
}

// site is a mirror on disk, keyed by slash-separated path relative to its root.
type site struct {
	root  string
	files map[string]*file
}

// skipTop are top-level entries that are not crawl output: dotfiles (.git,
// .gitignore, .mirrola-site) and Cloudflare Pages configuration.
var skipTop = map[string]bool{"_headers": true, "_redirects": true, "_routes.json": true, "_worker.js": true, "functions": true}

func loadSite(root string) (*site, error) {
	s := &site{root: root, files: map[string]*file{}}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if !strings.Contains(rel, "/") && (strings.HasPrefix(rel, ".") || skipTop[rel]) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		f, err := loadFile(p)
		if err != nil {
			return err
		}
		s.files[rel] = f
		return nil
	})
	return s, err
}

func loadFile(p string) (*file, error) {
	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".html", ".htm":
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		doc, err := html.Parse(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		return &file{
			hash:  sha256.Sum256(normalize(b)),
			page:  true,
			text:  visibleText(doc),
			links: htmlLinks(doc),
		}, nil
	case ".css":
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return &file{hash: sha256.Sum256(normalize(b)), links: cssLinks(string(b))}, nil
	case ".js", ".json", ".xml", ".txt":
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return &file{hash: sha256.Sum256(normalize(b))}, nil
	}
	fh, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return nil, err
	}
	f := &file{}
	copy(f.hash[:], h.Sum(nil))
	return f, nil
}

// noise is markup that changes between crawls without changing the page:
// asset version query strings (they change with every plugin update) and
// per-request values.
var noise = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`([?&](?:amp;)?ver=)[^"'&\s)\\]*`), "${1}"},
	{regexp.MustCompile(`(?i)(nonce\\?["']?\s*[:=]\s*\\?["']?)[a-f0-9]{6,}`), "${1}"},
	{regexp.MustCompile(`(?i)(_wpnonce=)[a-f0-9]{6,}`), "${1}"},
	// Simple Lightbox numbers its images randomly on every request.
	{regexp.MustCompile(`(data-slb-asset=")[0-9]+`), "${1}"},
	{regexp.MustCompile(`"[0-9]+":(\{"id":)`), `"":${1}`},
	// HTML/XML comments are invisible and often per request (sitemap plugins
	// write the generation time, a request id, timings and memory there).
	{regexp.MustCompile(`(?s)<!--.*?-->`), ""},
	// Whitespace-only changes (indentation, blank lines) don't matter; text
	// spacing is covered by the visible-text comparison.
	{regexp.MustCompile(`\s*([<>])\s*`), "${1}"},
	{regexp.MustCompile(`\s+`), " "},
}

func normalize(b []byte) []byte {
	for _, n := range noise {
		b = n.re.ReplaceAll(b, []byte(n.repl))
	}
	return bytes.TrimSpace(b)
}

// blocks start a new line of visible text.
var blocks = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "ul": true, "ol": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"table": true, "tr": true, "td": true, "th": true, "blockquote": true, "pre": true,
	"section": true, "article": true, "header": true, "footer": true, "nav": true,
	"aside": true, "main": true, "figure": true, "figcaption": true, "dt": true, "dd": true,
	"form": true, "option": true, "title": true, "hr": true,
}

// hidden elements carry no visible text.
var hidden = map[string]bool{"script": true, "style": true, "noscript": true, "template": true, "svg": true, "iframe": true}

// visibleText returns the text a reader sees (plus <title> and image alt
// text), one line per block, whitespace collapsed, empty lines dropped.
func visibleText(doc *html.Node) []string {
	var b strings.Builder
	var walk func(n *html.Node, inHead bool)
	walk = func(n *html.Node, inHead bool) {
		switch n.Type {
		case html.TextNode:
			if !inHead {
				b.WriteString(n.Data)
			}
			return
		case html.ElementNode:
			if hidden[n.Data] {
				return
			}
			if n.Data == "head" {
				inHead = true
			}
			if n.Data == "title" {
				inHead = false
			}
			if n.Data == "img" {
				if alt := attr(n, "alt"); strings.TrimSpace(alt) != "" {
					b.WriteString(" [img: " + alt + "] ")
				}
			}
			if blocks[n.Data] {
				b.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inHead)
		}
		if n.Type == html.ElementNode && blocks[n.Data] {
			b.WriteString("\n")
		}
	}
	walk(doc, false)

	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

var cssURL = regexp.MustCompile(`url\(\s*['"]?\s*([^'")]+?)\s*['"]?\s*\)`)

func cssLinks(css string) []string {
	var out []string
	for _, m := range cssURL.FindAllStringSubmatch(css, -1) {
		out = append(out, m[1])
	}
	return rootRelative(out)
}

// htmlLinks returns the root-relative links of a page: what a browser loads
// or navigates to on this site (href, src, srcset, poster, inline CSS).
func htmlLinks(doc *html.Node) []string {
	var out []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				switch a.Key {
				case "href", "src", "poster", "data-src":
					out = append(out, a.Val)
				case "srcset", "data-srcset":
					for _, c := range strings.Split(a.Val, ",") {
						if f := strings.Fields(c); len(f) > 0 {
							out = append(out, f[0])
						}
					}
				case "style":
					out = append(out, cssLinks(a.Val)...)
				}
			}
			if n.Data == "style" && n.FirstChild != nil {
				out = append(out, cssLinks(n.FirstChild.Data)...)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return rootRelative(out)
}

func rootRelative(links []string) []string {
	var out []string
	for _, l := range links {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "/") && !strings.HasPrefix(l, "//") {
			out = append(out, l)
		}
	}
	return out
}

// served returns the file a static host (Cloudflare Pages, server/) serves
// for a root-relative link: path decoded, query and fragment ignored,
// directory-like paths mapped to index.html. Empty if the link can't be parsed.
func served(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	p := u.Path
	if strings.HasSuffix(p, "/") {
		p += "index.html"
	} else if path.Ext(p) == "" {
		p += "/index.html"
	}
	return strings.TrimPrefix(path.Clean(p), "/")
}
