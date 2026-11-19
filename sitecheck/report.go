package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type textChange struct {
	page     string
	old, new []string // the differing lines, trimmed to a few
}

type report struct {
	oldFiles, newFiles, oldPages, newPages int

	removedPages, addedPages []string
	removedFiles, addedFiles []string
	textChanged              []textChange
	markupChanged            []string            // same text, different markup
	markupRemoved            []pattern           // tags gone from markup-changed pages
	markupAdded              []pattern           // tags new in markup-changed pages
	markupNumbers            []pattern           // number-only changes, as "context[old → new]context"
	assetsChanged            map[string][]string // by extension
	brokenNew                map[string][]string // target -> pages linking to it; not broken in old
	brokenOld                int                 // broken links already present in the baseline
	leaks                    map[string][]string // forbidden string -> files
}

func check(oldS, newS *site, forbid []string) *report {
	r := &report{
		oldFiles:      len(oldS.files),
		newFiles:      len(newS.files),
		assetsChanged: map[string][]string{},
		leaks:         map[string][]string{},
	}
	for p, f := range oldS.files {
		if f.page {
			r.oldPages++
		}
		if _, ok := newS.files[p]; !ok {
			if f.page {
				r.removedPages = append(r.removedPages, p)
			} else {
				r.removedFiles = append(r.removedFiles, p)
			}
		}
	}
	for p, nf := range newS.files {
		if nf.page {
			r.newPages++
		}
		of, ok := oldS.files[p]
		if !ok {
			if nf.page {
				r.addedPages = append(r.addedPages, p)
			} else {
				r.addedFiles = append(r.addedFiles, p)
			}
			continue
		}
		if of.hash == nf.hash {
			continue
		}
		switch {
		case nf.page && !equal(of.text, nf.text):
			o, n := diffLines(of.text, nf.text)
			r.textChanged = append(r.textChanged, textChange{p, o, n})
		case nf.page:
			r.markupChanged = append(r.markupChanged, p)
		default:
			ext := strings.ToLower(path.Ext(p))
			if ext == "" {
				ext = "(none)"
			}
			r.assetsChanged[ext] = append(r.assetsChanged[ext], p)
		}
	}

	removed, added, numbers := map[string][]string{}, map[string][]string{}, map[string][]string{}
	for _, p := range r.markupChanged {
		if rm, ad, nu, err := markupDelta(oldS.root, newS.root, p); err == nil {
			removed[p], added[p], numbers[p] = rm, ad, nu
		}
	}
	r.markupRemoved, r.markupAdded, r.markupNumbers = topPatterns(removed), topPatterns(added), topPatterns(numbers)

	oldBroken := brokenLinks(oldS)
	r.brokenOld = len(oldBroken)
	r.brokenNew = map[string][]string{}
	for target, from := range brokenLinks(newS) {
		if _, before := oldBroken[target]; !before {
			r.brokenNew[target] = from
		}
	}

	if len(forbid) > 0 {
		for p := range newS.files {
			if b, err := os.ReadFile(filepath.Join(newS.root, filepath.FromSlash(p))); err == nil {
				for _, s := range forbid {
					if strings.Contains(strings.ToLower(string(b)), strings.ToLower(s)) {
						r.leaks[s] = append(r.leaks[s], p)
					}
				}
			}
		}
	}

	sort.Strings(r.removedPages)
	sort.Strings(r.addedPages)
	sort.Strings(r.removedFiles)
	sort.Strings(r.addedFiles)
	sort.Strings(r.markupChanged)
	sort.Slice(r.textChanged, func(i, j int) bool { return r.textChanged[i].page < r.textChanged[j].page })
	for _, v := range r.assetsChanged {
		sort.Strings(v)
	}
	for _, v := range r.leaks {
		sort.Strings(v)
	}
	return r
}

// brokenLinks maps every root-relative link target that resolves to no file
// to the pages/stylesheets that contain it.
func brokenLinks(s *site) map[string][]string {
	out := map[string][]string{}
	for p, f := range s.files {
		for _, l := range f.links {
			target := served(l)
			if target == "" {
				continue
			}
			if _, ok := s.files[target]; ok {
				continue
			}
			key := strings.SplitN(strings.SplitN(l, "#", 2)[0], "?", 2)[0]
			if len(out[key]) == 0 || out[key][len(out[key])-1] != p {
				out[key] = append(out[key], p)
			}
		}
	}
	for _, v := range out {
		sort.Strings(v)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// diffLines trims the common head and tail and returns (up to 3 lines of)
// what is left on each side: enough to see what changed.
func diffLines(a, b []string) ([]string, []string) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	ja, jb := len(a), len(b)
	for ja > i && jb > i && a[ja-1] == b[jb-1] {
		ja--
		jb--
	}
	return head(a[i:ja], 3), head(b[i:jb], 3)
}

func head(l []string, n int) []string {
	out := []string{}
	for k, s := range l {
		if k == n {
			out = append(out, fmt.Sprintf("… (+%d lines)", len(l)-n))
			break
		}
		if r := []rune(s); len(r) > 160 {
			s = string(r[:160]) + "…"
		}
		out = append(out, s)
	}
	return out
}

// failed: something that must be looked at before publishing. Markup, asset
// and file changes are reported but don't fail (plugin updates cause them).
func (r *report) failed() bool {
	return len(r.removedPages) > 0 || len(r.textChanged) > 0 || len(r.brokenNew) > 0 || len(r.leaks) > 0
}

func (r *report) print(w io.Writer, max int) {
	fmt.Fprintf(w, "pages: %d -> %d, files: %d -> %d\n\n", r.oldPages, r.newPages, r.oldFiles, r.newFiles)

	list := func(title string, items []string, fail bool) {
		mark := "info"
		if fail && len(items) > 0 {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "[%s] %s: %d\n", mark, title, len(items))
		for k, s := range items {
			if max > 0 && k == max {
				fmt.Fprintf(w, "    … and %d more\n", len(items)-max)
				break
			}
			fmt.Fprintf(w, "    %s\n", s)
		}
	}

	list("pages removed", r.removedPages, true)

	mark := "info"
	if len(r.textChanged) > 0 {
		mark = "FAIL"
	}
	fmt.Fprintf(w, "[%s] pages with changed visible text: %d\n", mark, len(r.textChanged))
	for k, c := range r.textChanged {
		if max > 0 && k == max {
			fmt.Fprintf(w, "    … and %d more\n", len(r.textChanged)-max)
			break
		}
		fmt.Fprintf(w, "    %s\n", c.page)
		for _, l := range c.old {
			fmt.Fprintf(w, "      - %s\n", l)
		}
		for _, l := range c.new {
			fmt.Fprintf(w, "      + %s\n", l)
		}
	}

	targets := make([]string, 0, len(r.brokenNew))
	for t := range r.brokenNew {
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool {
		if a, b := len(r.brokenNew[targets[i]]), len(r.brokenNew[targets[j]]); a != b {
			return a > b
		}
		return targets[i] < targets[j]
	})
	var broken []string
	for _, t := range targets {
		from := r.brokenNew[t]
		broken = append(broken, fmt.Sprintf("%s  (linked from %d, e.g. %s)", t, len(from), from[0]))
	}
	list(fmt.Sprintf("new broken internal links (already broken in baseline: %d)", r.brokenOld), broken, true)

	var leaks []string
	for s, files := range r.leaks {
		leaks = append(leaks, fmt.Sprintf("%q in %d files, e.g. %s", s, len(files), files[0]))
	}
	sort.Strings(leaks)
	list("forbidden strings", leaks, true)

	list("pages added", r.addedPages, false)
	fmt.Fprintf(w, "[info] pages with changed markup only: %d", len(r.markupChanged))
	if len(r.markupChanged) > 0 {
		fmt.Fprintf(w, " (e.g. %s); what changed, by number of pages:", r.markupChanged[0])
	}
	fmt.Fprintln(w)
	patterns := func(sign string, ps []pattern) {
		for k, p := range ps {
			if max > 0 && k == max {
				fmt.Fprintf(w, "    %s … and %d more\n", sign, len(ps)-max)
				break
			}
			fmt.Fprintf(w, "    %s %5d  %s\n", sign, p.pages, p.tag)
		}
	}
	patterns("-", r.markupRemoved)
	patterns("+", r.markupAdded)
	patterns("~", r.markupNumbers)

	exts := make([]string, 0, len(r.assetsChanged))
	for e := range r.assetsChanged {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	var assets []string
	for _, e := range exts {
		files := r.assetsChanged[e]
		assets = append(assets, fmt.Sprintf("%s: %d, e.g. %s", e, len(files), files[0]))
	}
	list("changed files by type", assets, false)
	list("files removed", r.removedFiles, false)
	list("files added", r.addedFiles, false)

	if r.failed() {
		fmt.Fprintln(w, "\nRESULT: FAIL, look at the [FAIL] sections before publishing")
	} else {
		fmt.Fprintln(w, "\nRESULT: OK, visible content and internal links are intact")
	}
}
