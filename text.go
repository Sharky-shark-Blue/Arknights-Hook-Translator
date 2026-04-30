package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
)

var (
	unicodeLine = regexp.MustCompile(`^\[unicode\]\s+(.+)$`)
	unicodeUnit = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)
	htmlTag     = regexp.MustCompile(`<[^>]+>`)

	rePureNumber       = regexp.MustCompile(`^\d+$`)
	reSlashNumber      = regexp.MustCompile(`^/\d+$`)
	reNumberPair       = regexp.MustCompile(`^\d+/\d+$`)
	rePercent          = regexp.MustCompile(`^\d{1,3}%$`)
	reTimeText         = regexp.MustCompile(`^\d+[日時間分秒]+$`)
	reEnglishOnly      = regexp.MustCompile(`^[A-Za-z0-9_./:\- ]+$`)
	reNumberSymbolOnly = regexp.MustCompile(`^[0-9\s\/:%.\-+]+$`)
	reJapaneseOrHan    = regexp.MustCompile(`[\p{Han}\x{3040}-\x{30ff}]`)
)

func decodeUnicodeEscaped(s string) string {
	matches := unicodeUnit.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return ""
	}
	units := make([]uint16, 0, len(matches))
	for _, m := range matches {
		var v uint16
		if _, err := fmt.Sscanf(m[1], "%04x", &v); err == nil {
			units = append(units, v)
		}
	}
	return string(utf16.Decode(units))
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

func stripRichText(text string) string {
	return strings.TrimSpace(htmlTag.ReplaceAllString(text, ""))
}

func displayTextOnly(text string) {
	text = normalizeText(stripRichText(text))
	if text == "" {
		return
	}
	clearConsole()
	fmt.Println(text)
}

func shouldSaveUntranslated(text string) bool {
	plain := stripRichText(normalizeText(text))
	if plain == "" {
		return false
	}
	if rePureNumber.MatchString(plain) || reSlashNumber.MatchString(plain) ||
		reNumberPair.MatchString(plain) || rePercent.MatchString(plain) ||
		reTimeText.MatchString(plain) || reEnglishOnly.MatchString(plain) ||
		reNumberSymbolOnly.MatchString(plain) {
		return false
	}
	return reJapaneseOrHan.MatchString(plain)
}

func handleUnicodeLine(line string) bool {
	m := unicodeLine.FindStringSubmatch(line)
	if len(m) != 2 {
		return false
	}
	decoded := normalizeText(decodeUnicodeEscaped(m[1]))
	if decoded == "" {
		return true
	}
	addUntranslatedToTrans(decoded)
	if zh := localTranslate(decoded); zh != "" {
		displayTextOnly(zh)
	} else {
		displayTextOnly(decoded)
	}
	return true
}

func handleLine(line string) {
	if handleUnicodeLine(line) {
		return
	}
	if strings.Contains(line, "Failed to attach") ||
		strings.Contains(line, "Unable to find process") ||
		strings.Contains(line, "unable to connect") ||
		strings.Contains(line, "[ERROR]") {
		fmt.Println(line)
	}
}
