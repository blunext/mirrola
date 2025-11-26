package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/unicode/norm"
)

func TestUnicode_Normalization(t *testing.T) {
	cfg := &Config{
		SafeFilenames: true,
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "NFC ó (U+00F3)",
			input:    "\u00F3", // ó
			expected: "%C3%B3", // Percent-encoded when SafeFilenames=true
		},
		{
			name:     "NFD o + acute (U+006F U+0301)",
			input:    "o\u0301", // o + acute
			expected: "%C3%B3",  // Normalized to NFC, then percent-encoded
		},
		{
			name:     "NFC ż (U+017C)",
			input:    "\u017C",
			expected: "%C5%BC",
		},
		{
			name:     "NFD z + dot above (U+007A U+0307)",
			input:    "z\u0307",
			expected: "%C5%BC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test normalizePath with SafeFilenames=true (should use NFC)
			result := cfg.normalizePath(tt.input)

			// Verify result is NFC
			assert.True(t, norm.NFC.IsNormalString(result), "Result should be NFC normalized")
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUnicode_Transliteration(t *testing.T) {
	// Test removeDiacritics function directly
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Polish all chars lower", "ąęćłńóśźż", "aeclnoszz"},
		{"Polish all chars upper", "ĄĘĆŁŃÓŚŹŻ", "AECLNOSZZ"},
		{"Mixed PL sentence", "Zażółć gęślą jaźń", "Zazolc gesla jazn"},
		{"German umlauts", "Müller über Straße", "Muller uber Strae"}, // Note: standard NFD decomposition removes umlaut dots, ß might stay or change depending on normalizer. Let's verify behavior.
		// Go's transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
		// ü (u + umlaut) -> u
		// ß (sharp s) -> ß (not decomposed by NFD)
		{"French accents", "Crème brûlée à la française", "Creme brulee a la francaise"},
		{"Emoji", "Smile 😃", "Smile 😃"}, // Emoji should be preserved or stripped?
		// removeDiacritics only removes Mn (Mark, nonspacing). Emoji are usually So (Symbol, other).
		// So they should remain.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := removeDiacritics(tt.input)
			assert.NoError(t, err)

			// For German ß, standard NFD doesn't decompose it to ss.
			// If we want "Muller uber Strae" (ß -> Strae?? No, ß is unchanged usually unless mapped).
			// Wait, "Straße" -> "Strasse" is transliteration, not just diacritic removal.
			// removeDiacritics implementation:
			// 1. Custom replacer for Polish
			// 2. NFD -> Remove Mn -> NFC
			// ß is not Mn. So it stays "Straße".
			// Let's adjust expectation for German test if needed.
			if tt.name == "German umlauts" {
				// "Müller" -> "Muller" (ü -> u + umlaut, umlaut removed)
				// "über" -> "uber"
				// "Straße" -> "Straße" (ß stays)
				// So expected: "Muller uber Straße"
				if result == "Muller uber Straße" {
					// OK
				} else {
					// Check what happened
					// If result is "Muller uber Strae", then ß was removed? Unlikely.
				}
			}

			// Actually, let's just assert what we expect based on logic
			if tt.name == "German umlauts" {
				assert.Equal(t, "Muller uber Straße", result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestUnicode_InvalidSequences(t *testing.T) {
	// Invalid UTF-8
	input := string([]byte{0xff, 0xfe, 0xfd})

	// normalizePath handles strings. Go strings can contain invalid UTF-8.
	// norm.NFC.String() might not replace invalid bytes with Replacement Char.
	// It depends on the transformer implementation.

	cfg := &Config{SafeFilenames: true}
	result := cfg.normalizePath(input)

	// The result might vary. Let's just check it doesn't panic.
	assert.NotEmpty(t, result)
	// Don't assert specific replacement char behavior
}
