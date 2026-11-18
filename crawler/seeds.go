package crawler

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Besides following links from the start page, a crawl of a site root also
// fetches what nothing links to but a static copy still needs:
//   - /robots.txt, and every sitemap it lists (Sitemap: lines). Sitemaps are
//     followed recursively (sitemap index -> sitemaps -> pages), so pages that
//     no other page links to get mirrored too; the sitemap's XSL stylesheet is
//     saved with it.
//   - the site's 404 page, saved as /404.html: static hosts (Cloudflare
//     Pages) serve it for missing paths. Without it Pages answers every
//     unknown URL with the home page and status 200.

// notFoundPath is requested to capture the 404 page; it must not exist.
const notFoundPath = "/mirrola-404-check/"

type taskKind int

const (
	taskNormal   taskKind = iota
	taskOptional          // a 404 just means the site doesn't have it (robots.txt)
	taskNotFound          // expect a 404 page and save it as /404.html
)

// enqueueSeeds queues robots.txt and the 404 probe when the crawl starts at
// the site root (robots.txt and 404.html belong to the whole host, not to a
// subdirectory being crawled).
func enqueueSeeds(ctx context.Context, tasks chan<- task) error {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return err
	}
	if p := strings.TrimSuffix(base.Path, "/"); p != "" {
		return nil
	}
	root := base.Scheme + "://" + base.Host
	if err := enqueue(ctx, task{url: root + "/robots.txt", from: "(robots.txt probe)", kind: taskOptional}, tasks); err != nil {
		return err
	}
	return enqueue(ctx, task{url: root + notFoundPath, from: "(404 page probe)", kind: taskNotFound}, tasks)
}

func isRobots(link string) bool {
	u, err := url.Parse(link)
	return err == nil && u.Path == "/robots.txt"
}

var (
	reRobotsSitemap = regexp.MustCompile(`(?im)^[ \t]*sitemap[ \t]*:[ \t]*(\S+)`)
	reSitemapLoc    = regexp.MustCompile(`(?is)<loc>\s*(.*?)\s*</loc>`)
	reXMLStylesheet = regexp.MustCompile(`(?is)<\?xml-stylesheet\b[^>]*?\bhref\s*=\s*["']([^"']+)["']`)
	reSitemapRoot   = regexp.MustCompile(`(?i)<(urlset|sitemapindex)\b`)
)

// discover returns the same-host URLs a robots.txt or XML sitemap points to:
// pages and further sitemaps (links), and the sitemap's XSL stylesheet
// (assets). Anything else yields nothing.
func discover(link, ct string, body []byte) (links, assets []string) {
	s := string(body)
	var rawLinks, rawAssets []string
	switch {
	case isRobots(link):
		for _, m := range reRobotsSitemap.FindAllStringSubmatch(s, -1) {
			rawLinks = append(rawLinks, m[1])
		}
	case (ct == "text/xml" || ct == "application/xml") && reSitemapRoot.MatchString(s):
		for _, m := range reSitemapLoc.FindAllStringSubmatch(s, -1) {
			rawLinks = append(rawLinks, html.UnescapeString(strings.TrimPrefix(strings.TrimSuffix(m[1], "]]>"), "<![CDATA[")))
		}
		for _, m := range reXMLStylesheet.FindAllStringSubmatch(s, -1) {
			rawAssets = append(rawAssets, html.UnescapeString(m[1]))
		}
	}
	return sameHostURLs(link, rawLinks), sameHostURLs(link, rawAssets)
}

func sameHostURLs(from string, refs []string) []string {
	base, err := url.Parse(from)
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range refs {
		u, err := base.Parse(strings.TrimSpace(r))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		u.Fragment = ""
		if cfg.SameHost(u.String(), cfg.BaseURL) {
			out = append(out, u.String())
		}
	}
	return out
}

// processNotFound fetches a path that doesn't exist and saves the site's 404
// page as /404.html. A site without a proper 404 (status 200, or not HTML)
// only gets a warning: the mirror is still complete.
func processNotFound(ctx context.Context, t task, tasks chan<- task) error {
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
	}
	resp, err := fetch(ctx, t.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	switch {
	case resp.StatusCode != http.StatusNotFound:
		fmt.Printf("[WARN] %s returned %s instead of 404; no 404.html saved\n", t.url, resp.Status)
		return nil
	case !strings.HasPrefix(ct, "text/html"):
		fmt.Printf("[WARN] the 404 page at %s is %q, not HTML; no 404.html saved\n", t.url, ct)
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	final := t.url
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	page, err := url.Parse(t.url)
	if err != nil {
		return err
	}
	page.Path = "/404.html"
	assets, links, err := processHTMLAt(ctx, page.String(), final, body)
	if err != nil {
		return err
	}
	for _, a := range assets {
		if err := enqueueLink(ctx, a, page.String(), t.depth, tasks); err != nil {
			fmt.Printf("[ERROR] Failed to enqueue asset %s: %v\n", a, err)
		}
	}
	for _, l := range links {
		if err := enqueueLink(ctx, l, page.String(), t.depth+1, tasks); err != nil {
			fmt.Printf("[ERROR] Failed to enqueue link %s: %v\n", l, err)
		}
	}
	return nil
}
