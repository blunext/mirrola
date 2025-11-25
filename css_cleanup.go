package main

import (
	"regexp"
	"strings"
)

// removeCSSComments strips CSS comments (/* ... */) from CSS content
func removeCSSComments(css string) string {
	// Remove multiline CSS comments /* ... */
	reComment := regexp.MustCompile(`/\*[^*]*\*+(?:[^/*][^*]*\*+)*/`)
	return reComment.ReplaceAllString(css, "")
}

// removeCDATAMarkers strips CDATA markers from JavaScript/CSS content
func removeCDATAMarkers(content string) string {
	// Remove //<![CDATA[ and //]]> (JavaScript style)
	content = strings.ReplaceAll(content, "//<![CDATA[", "")
	content = strings.ReplaceAll(content, "//]]>", "")
	// Remove /* <![CDATA[ */ and /* ]]> */ (CSS style)
	content = strings.ReplaceAll(content, "/* <![CDATA[ */", "")
	content = strings.ReplaceAll(content, "/* ]]> */", "")
	return content
}
