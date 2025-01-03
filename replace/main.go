package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"
)

// Replace definiuje pojedynczą regułę zastępowania.
type Replace struct {
	oldText string
	newText string
}

// ReplaceList to lista reguł zastępowania.
type ReplaceList []Replace

// Zdefiniowane reguły – można je rozszerzać według potrzeb.
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

// applyRules przechodzi po liście reguł i zamienia wystąpienia oldText na newText.
func applyRules(text string) string {
	for _, r := range rules {
		text = strings.ReplaceAll(text, r.oldText, r.newText)
	}
	return text
}

func main() {
	// Parsowanie flag wejścia i wyjścia.
	inputFile := flag.String("input", "/Users/blt1wz/priv/mirrola/wordpress/wordpress_db-oryg.sql", "Plik wejściowy SQL")
	outputFile := flag.String("output", "/Users/blt1wz/priv/mirrola/wordpress/wordpress.sql", "Plik wyjściowy SQL")
	flag.Parse()

	if *inputFile == "" || *outputFile == "" {
		log.Fatal("Musisz podać flagi -input oraz -output")
	}

	data, err := ioutil.ReadFile(*inputFile)
	if err != nil {
		log.Fatalf("Błąd podczas odczytu pliku: %v", err)
	}
	content := string(data)

	// Wyrażenie regularne do znajdowania ciągów serializowanych PHP.
	// Wzorzec dopasowuje ciągi postaci:
	// s:<liczba>:"treść";
	// lub z escape’owanymi cudzysłowami, np.: s:<liczba>:\"treść\";
	// (?s) powoduje, że kropka dopasowuje również znaki nowej linii.
	re := regexp.MustCompile(`(?s)s:(\d+):(\\?"|")(.+?)(\\?"|");`)
	newContent := re.ReplaceAllStringFunc(content, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 5 {
			return match
		}
		// submatches[1] – oryginalna długość (jako string)
		// submatches[2] – otwierający cudzysłów (może być z backslashem)
		// submatches[3] – zawartość serializowanego ciągu
		// submatches[4] – zamykający cudzysłów
		origLengthStr := submatches[1]
		openingQuote := submatches[2]
		innerText := submatches[3]
		closingQuote := submatches[4]

		// Zastąpienie ciągu wg reguł.
		replacedText := applyRules(innerText)

		// Obliczenie nowej długości (w bajtach – zgodnie z PHP)
		newLength := len(replacedText)

		// Dla informacji, można wypisać oryginalną długość:
		_ = origLengthStr // (nieużywane, ale można logować, jeśli potrzeba)

		// Rekonstruowanie fragmentu z nową długością.
		return fmt.Sprintf("s:%d:%s%s%s;", newLength, openingQuote, replacedText, closingQuote)
	})

	// Dla części, które nie były serializacjami – wykonaj globalne zastąpienie.
	finalContent := applyRules(newContent)

	if err = os.WriteFile(*outputFile, []byte(finalContent), 0644); err != nil {
		log.Fatalf("Błąd podczas zapisu pliku: %v", err)
	}
}
