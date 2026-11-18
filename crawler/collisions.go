package crawler

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
)

// outputRegistry remembers which source URL produced each output file, so that
// two different URLs mapping to the same path are detected instead of silently
// overwriting each other: "łódź.jpg" and "lodz.jpg" after transliteration, or
// "style.css?ver=1" and "?ver=2" (the query is not part of the file name; a
// static host serves both as one file).
type outputRegistry struct {
	mu         sync.Mutex
	owners     map[string]string // output path -> canonical source URL
	collisions []string
}

func newOutputRegistry() *outputRegistry {
	return &outputRegistry{owners: make(map[string]string)}
}

// errOutputCollision is returned by claimOutput in strict mode.
type errOutputCollision struct {
	Path, First, Second string
}

func (e *errOutputCollision) Error() string {
	return fmt.Sprintf("output collision: %s and %s both map to %s", e.First, e.Second, e.Path)
}

// canonicalSource returns a key identifying the resource behind a URL.
// URLs that differ only in Unicode normalization (NFC vs NFD) or host case
// refer to the same resource and must not be reported as a collision.
func canonicalSource(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	p := u.Path
	if p == "" {
		p = "/" // https://host and https://host/ are the same page
	}
	key := strings.ToLower(u.Host) + norm.NFC.String(p)
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key
}

// claimOutput registers link as the source of outPath. It returns true when
// the caller should write the file: the path is new. It returns false when
// the path was already claimed, either by the same resource (duplicate fetch)
// or by a different one (collision: logged, first writer wins). In strict mode
// a collision is returned as an error. A Config without a registry (e.g. built
// directly in tests) disables tracking.
func (c *Config) claimOutput(outPath, link string) (bool, error) {
	if err := c.checkNotHidden(outPath); err != nil {
		return false, err
	}
	r := c.outputs
	if r == nil {
		return true, nil
	}
	src := canonicalSource(link)

	r.mu.Lock()
	defer r.mu.Unlock()
	owner, taken := r.owners[outPath]
	if !taken {
		r.owners[outPath] = src
		return true, nil
	}
	if owner == src {
		return false, nil
	}
	collision := &errOutputCollision{Path: outPath, First: owner, Second: src}
	r.collisions = append(r.collisions, collision.Error())
	if c.Strict {
		return false, collision
	}
	fmt.Printf("[WARN] %s (keeping the first)\n", collision.Error())
	return false, nil
}

// hiddenPathError is returned for an output path with a component starting
// with "." (e.g. a page linking to /.git/config or /.gitignore).
type hiddenPathError struct{ Path string }

func (e *hiddenPathError) Error() string {
	return "refusing to write hidden path " + e.Path + " (it would land in the output repo's .git, .gitignore or another dotfile)"
}

// checkNotHidden rejects output paths with a hidden component. The output
// directory is a git repo: writing /.git/config or /.gitignore from a crawled
// link would corrupt it, and make clean-site never removes dotfiles either.
func (c *Config) checkNotHidden(outPath string) error {
	rel, err := filepath.Rel(c.OutputDir, outPath)
	if err != nil {
		rel = outPath
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return &hiddenPathError{Path: rel}
		}
	}
	return nil
}

// Collisions returns the collisions detected so far.
func (c *Config) Collisions() []string {
	if c.outputs == nil {
		return nil
	}
	c.outputs.mu.Lock()
	defer c.outputs.mu.Unlock()
	return append([]string(nil), c.outputs.collisions...)
}
