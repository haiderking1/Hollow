package browser

import (
	"strings"
)

const (
	MaxScrapeBytes = 50 * 1024
	MaxScrapeLines = 2000
)

func truncateScrape(text string) (string, bool) {
	totalBytes := len(text)

	lines := strings.Split(text, "\n")
	if len(lines) > 0 && text != "" && strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)

	if totalLines <= MaxScrapeLines && totalBytes <= MaxScrapeBytes {
		return text, false
	}

	var outputLines []string
	outputBytesCount := 0
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(outputLines) < MaxScrapeLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if len(outputLines) > 0 {
			lineBytes += 1 // +1 for newline
		}

		if outputBytesCount+lineBytes > MaxScrapeBytes {
			if len(outputLines) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, MaxScrapeBytes)
				outputLines = append(outputLines, truncatedLine)
				lastLinePartial = true
			}
			break
		}

		outputLines = append([]string{line}, outputLines...)
		outputBytesCount += lineBytes
	}

	_ = lastLinePartial
	return strings.Join(outputLines, "\n"), true
}

func truncateStringToBytesFromEnd(str string, maxBytes int) string {
	if len(str) <= maxBytes {
		return str
	}
	start := len(str) - maxBytes
	for start < len(str) && (str[start]&0xC0) == 0x80 {
		start++
	}
	return str[start:]
}
