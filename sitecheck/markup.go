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

// splitTags splits normalized HTML into tags (with the text that follows
// each), whitespace collapsed and the page's own path replaced by {page}, so
// the same template change on different pages reads the same.
func splitTags(b []byte, pagePath string) []string {
	s := string(normalize(b))
	if pagePath != "" && pagePath != "/" {
		s = strings.ReplaceAll(s, url.QueryEscape(pagePath), "{page}")
		s = strings.ReplaceAll(s, pagePath, "{page}")
	}
	var out []string
	for _, t := range strings.Split(s, "<") {
		if t = strings.Join(strings.Fields(t), " "); t != "" {
			out = append(out, "<"+t)
		}
	}
	return out
}

// maskNumbers replaces numbers with # (keeping %XX escapes), so tags that
// differ only by ids, sizes or versions count as one pattern.
func maskNumbers(t string) string {
	return digits.ReplaceAllStringFunc(t, func(m string) string {
		if m[0] == '%' {
			return m
		}
		return "#"
	})
}

func shorten(t string, n int) string {
	if r := []rune(t); len(r) > n {
		return string(r[:n]) + "…"
	}
	return t
}

// tags counts a page's tags with numbers masked and long tags shortened.
func tags(b []byte, pagePath string) map[string]int {
	out := map[string]int{}
	for _, t := range splitTags(b, pagePath) {
		out[shorten(maskNumbers(t), 150)]++
	}
	return out
}

// markupDelta returns the tags only in the old page and only in the new one
// (order ignored), so the same template change on many pages adds up to one
// pattern. When the pages differ only in numbers (both lists empty once
// numbers are masked), numbers returns each changed number with a little
// context instead, e.g. `"wordpress 3.1.6 [7.1.4 → 7.1.5]"},"disqus`.
func markupDelta(oldRoot, newRoot, rel string) (removed, added, numbers []string, err error) {
	ob, err := os.ReadFile(filepath.Join(oldRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil, nil, err
	}
	nb, err := os.ReadFile(filepath.Join(newRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil, nil, err
	}
	pagePath := "/" + strings.TrimSuffix(path.Dir(rel), ".") + "/"
	pagePath = strings.ReplaceAll(pagePath, "//", "/")
	ot, nt := splitTags(ob, pagePath), splitTags(nb, pagePath)
	o, n := map[string]int{}, map[string]int{}
	for _, t := range ot {
		o[shorten(maskNumbers(t), 150)]++
	}
	for _, t := range nt {
		n[shorten(maskNumbers(t), 150)]++
	}
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
	if len(removed) == 0 && len(added) == 0 {
		numbers = numberChanges(ot, nt)
	}
	return removed, added, numbers, nil
}

// numberChanges pairs the tags that changed only in numbers (same tag once
// numbers are masked) and describes each change by its differing fragment.
func numberChanges(oldTags, newTags []string) []string {
	count := map[string]int{}
	for _, t := range oldTags {
		count[t]++
	}
	for _, t := range newTags {
		count[t]--
	}
	byMask := map[string][]string{} // tags only in the new page, by masked form
	for _, t := range newTags {
		if count[t] < 0 {
			byMask[maskNumbers(t)] = append(byMask[maskNumbers(t)], t)
			count[t]++
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range oldTags {
		if count[t] <= 0 {
			continue
		}
		count[t]--
		m := maskNumbers(t)
		if cands := byMask[m]; len(cands) > 0 {
			if f := numberFragment(t, cands[0]); !seen[f] {
				seen[f] = true // a pattern counts pages, not occurrences
				out = append(out, f)
			}
			byMask[m] = cands[1:]
		}
	}
	return out
}

// numberFragment shows where two strings that differ only in numbers
// differ: the whole changed number (or version like 7.1.4) as [old → new],
// with up to 30 characters before and 15 after.
func numberFragment(a, b string) string {
	ra, rb := []rune(a), []rune(b)
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	ja, jb := len(ra), len(rb)
	for ja > i && jb > i && ra[ja-1] == rb[jb-1] {
		ja--
		jb--
	}
	num := func(r rune) bool { return r >= '0' && r <= '9' || r == '.' }
	for i > 0 && num(ra[i-1]) {
		i--
	}
	for ja < len(ra) && num(ra[ja]) {
		ja++
		jb++
	}
	left, right := ra[max(0, i-30):i], ra[ja:min(len(ra), ja+15)]
	frag := string(left) + "[" + string(ra[i:ja]) + " → " + string(rb[i:jb]) + "]" + string(right)
	if i > 30 {
		frag = "…" + frag
	}
	if ja+15 < len(ra) {
		frag += "…"
	}
	return frag
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
