// Command sitecheck compares two static mirrors (the published one and a fresh
// crawl, e.g. of the local WordPress after a plugin or core update) and checks
// the new one for broken internal links. Exit status 1 means something needs a
// look before publishing; 2 means sitecheck itself failed.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	oldDir := flag.String("old", "", "baseline mirror (e.g. the last published one)")
	newDir := flag.String("new", "", "mirror to check")
	forbid := flag.String("forbid", "", "comma-separated strings that must not appear in -new (e.g. localhost:8000)")
	maxList := flag.Int("max", 20, "max entries listed per section (0 = all)")
	flag.Parse()
	if *oldDir == "" || *newDir == "" {
		fmt.Fprintln(os.Stderr, "usage: sitecheck -old DIR -new DIR [-forbid a,b] [-max N]")
		os.Exit(2)
	}

	oldSite, err := loadSite(*oldDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sitecheck:", err)
		os.Exit(2)
	}
	newSite, err := loadSite(*newDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sitecheck:", err)
		os.Exit(2)
	}

	var needles []string
	for _, s := range strings.Split(*forbid, ",") {
		if s = strings.TrimSpace(s); s != "" {
			needles = append(needles, s)
		}
	}

	r := check(oldSite, newSite, needles)
	r.print(os.Stdout, *maxList)
	if r.failed() {
		os.Exit(1)
	}
}
