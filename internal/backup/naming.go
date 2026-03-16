package backup

import (
	"regexp"
	"strings"
	"time"
)

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func BuildFilename(prefix string, now time.Time) string {
	safePrefix := sanitizeFilenamePrefix(prefix)
	timestamp := now.UTC().Format("20060102T150405Z")
	return safePrefix + "_" + timestamp + ".dump"
}

func sanitizeFilenamePrefix(prefix string) string {
	cleaned := strings.TrimSpace(prefix)
	cleaned = unsafeFilenameChars.ReplaceAllString(cleaned, "-")
	cleaned = strings.Trim(cleaned, "-_.")
	if cleaned == "" {
		return "postgres-backup"
	}

	return cleaned
}
