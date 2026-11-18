package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// Define the -dir flag for specifying the directory to serve
	dir := flag.String("dir", "./static", "the directory to serve")
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, *dir)
	})

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("Serving %s on http://localhost%s\n", *dir, addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func serveFile(w http.ResponseWriter, r *http.Request, baseDir string) {
	requestPath := r.URL.Path

	// Build the full path to the file on the disk
	fullPath := filepath.Join(baseDir, filepath.FromSlash(requestPath))

	// Check if the requested path refers to a directory
	// If it's a directory (e.g. /category/relacje/), we should serve index.html
	info, err := os.Stat(fullPath)
	if err == nil && info.IsDir() {
		// It's a directory, append index.html
		fullPath = filepath.Join(fullPath, "index.html")
	}

	// If the path does not end with a slash but actually refers to a directory,
	// redirect the user to a slash-terminated path. This ensures a trailing slash
	// is always used for directories.
	if err == nil && info.IsDir() && !strings.HasSuffix(requestPath, "/") {
		http.Redirect(w, r, requestPath+"/", http.StatusMovedPermanently)
		return
	}

	// Check if the file (or index.html) exists
	info, err = os.Stat(fullPath)
	if err != nil {
		// File does not exist, return 404
		http.NotFound(w, r)
		return
	}

	// Serve the found file
	http.ServeFile(w, r, fullPath)
}
