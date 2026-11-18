package main

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// digits matches numbers, and %XX escapes so they can be kept as they are.
var digits = regexp.MustCompile(`%[0-9A-Fa-f]{2}|[0-9]+`)

// tags splits normalized HTML into tags (with the text that follows each),
// masks what differs between pages for the same template change (numbers,
// the page's own path) and counts them.
func tags(b []byte, pagePath string) map[string]int {
	s := string(normalize(b))
	if pagePath != "" && pagePath != "/" {
		s = strings.ReplaceAll(s, url.QueryEscape(pagePath), "{page}")
		s = strings.ReplaceAll(s, pagePath, "{page}")
	}
	out := map[string]int{}
	for _, t := range strings.Split(s, "<") {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" {
			continue
		}
		t = digits.ReplaceAllStringFunc(t, func(m string) string {
			if m[0] == '%' {
				return m
			}
			return "#"
		})
		if r := []rune(t); len(r) > 150 {
			t = string(r[:150]) + "…"
		}
		out["<"+t]++
	}
	return out
}

// markupDelta returns the tags only in the old page and only in the new one
// (order ignored), so the same template change on many pages adds up to one
// pattern.
func markupDelta(oldRoot, newRoot, rel string) (removed, added []string, err error) {
	ob, err := os.ReadFile(filepath.Join(oldRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil, err
	}
	nb, err := os.ReadFile(filepath.Join(newRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil, err
	}
	pagePath := "/" + strings.TrimSuffix(path.Dir(rel), ".") + "/"
	pagePath = strings.ReplaceAll(pagePath, "//", "/")
	o, n := tags(ob, pagePath), tags(nb, pagePath)
	for t, c := range o {
		if n[t] < c {
			removed = append(removed, t)
		}
	}
	for t, c := range n {
		if o[t] < c {
			added = append(added, t)
		}
	}
	return removed, added, nil
}

type pattern struct {
	tag   string
	pages int
}

// topPatterns counts, per tag, on how many pages it was removed/added.
func topPatterns(perPage map[string][]string) []pattern {
	count := map[string]int{}
	for _, ts := range perPage {
		for _, t := range ts {
			count[t]++
		}
	}
	out := make([]pattern, 0, len(count))
	for t, c := range count {
		out = append(out, pattern{t, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pages != out[j].pages {
			return out[i].pages > out[j].pages
		}
		return out[i].tag < out[j].tag
	})
	return out
}
