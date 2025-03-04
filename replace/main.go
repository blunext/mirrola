package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
)

// Replace defines a single replacement rule.
type Replace struct {
	oldText string
	newText string
}

// ReplaceList is a list of replacement rules.
type ReplaceList []Replace

// Defined rules – can be extended as needed.
var rules = ReplaceList{
	{"https://olamundo.pl", "http://localhost:8000"},
	{"HTTPS://OLAMUNDO.PL", "http://localhost:8000"},
	{"https://www.olamundo.pl", "http://localhost:8000"},
	{"http://olamundo.pl", "http://localhost:8000"},
	{"HTTP://OLAMUNDO.PL", "http://localhost:8000"},
	{`http:\\/\\/olamundo.pl`, `http:\/\/localhost:8000`},
	{`https:\\/\\/olamundo.pl`, `http:\/\/localhost:8000`},

	{"/home/olamundo/domains/olamundo.pl/public_html", "/var/www/html"},
	{`\\/home\\/olamundo\\/domains\\/olamundo.pl\\/public_html`, `\/var\/www\/html`},
}

// applyRules iterates through the list of rules and replaces occurrences of oldText with newText.
func applyRules(text string) string {
	for _, r := range rules {
		text = strings.ReplaceAll(text, r.oldText, r.newText)
	}
	return text
}

func main() {
	// Parse input and output file flags.
	inputFile := flag.String("input", "/Users/blt1wz/priv/mirrola/wordpress/wordpress_db-oryg.sql", "Input SQL file")
	outputFile := flag.String("output", "/Users/blt1wz/priv/mirrola/wordpress/wordpress.sql", "Output SQL file")
	flag.Parse()

	if *inputFile == "" || *outputFile == "" {
		log.Fatal("You must specify both -input and -output flags")
	}

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		log.Fatalf("Error reading file: %v", err)
	}
	content := string(data)

	// Regular expression to find serialized PHP strings.
	// The pattern matches strings in the format:
	// s:<number>:"content";
	// or with escaped quotes, e.g.: s:<number>:\"content\";
	// (?s) ensures that the dot also matches newlines.
	re := regexp.MustCompile(`(?s)s:(\d+):(\\?"|")(.+?)(\\?"|");`)
	newContent := re.ReplaceAllStringFunc(content, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 5 {
			return match
		}
		// submatches[1] – original length (as a string)
		// submatches[2] – opening quote (may be escaped)
		// submatches[3] – serialized string content
		// submatches[4] – closing quote
		origLengthStr := submatches[1]
		openingQuote := submatches[2]
		innerText := submatches[3]
		closingQuote := submatches[4]

		// Replace the string according to the rules.
		replacedText := applyRules(innerText)

		// Calculate the new length (in bytes – as per PHP)
		newLength := len(replacedText)

		// For debugging purposes, the original length can be logged if needed:
		_ = origLengthStr // (unused, but can be logged if necessary)

		// Reconstruct the segment with the new length.
		return fmt.Sprintf("s:%d:%s%s%s;", newLength, openingQuote, replacedText, closingQuote)
	})

	// Apply global replacements to parts that are not serialized strings.
	finalContent := applyRules(newContent)

	if err = os.WriteFile(*outputFile, []byte(finalContent), 0644); err != nil {
		log.Fatalf("Error writing file: %v", err)
	}
}
