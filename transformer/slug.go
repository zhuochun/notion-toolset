package transformer

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
)

const MaxSlugLength = 120

type SlugRegistry struct {
	mu    sync.Mutex
	cache map[string]string
}

func SlugifyTitle(title string, maxLen int) string {
	title = strings.TrimSpace(strings.ToLower(title))
	if title == "" || maxLen <= 0 {
		return ""
	}

	var builder strings.Builder
	lastDash := false

	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastDash = true
			}
		default:
			if isInvalidFilenameRune(r) {
				continue
			}
		}
	}

	slug := strings.Trim(builder.String(), "-")
	slug = strings.TrimRight(slug, ". ")
	return truncateRunes(slug, maxLen)
}

func (r *SlugRegistry) Register(slug string, pageID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cache == nil {
		r.cache = make(map[string]string)
	}

	original := slug
	candidate := slug
	suffix := SimpleID(pageID)
	counter := 0

	for {
		if isReservedWindowsBasename(candidate) {
			counter++
			suffixValue := suffix
			if counter > 1 {
				suffixValue = fmt.Sprintf("%s-%d", suffix, counter)
			}
			candidate = appendSlugSuffix(original, suffixValue, MaxSlugLength)
			continue
		}

		if owner, exists := r.cache[candidate]; exists {
			if owner == pageID {
				return candidate
			}

			counter++
			suffixValue := suffix
			if counter > 1 {
				suffixValue = fmt.Sprintf("%s-%d", suffix, counter)
			}
			candidate = appendSlugSuffix(original, suffixValue, MaxSlugLength)
			continue
		}

		r.cache[candidate] = pageID
		return candidate
	}
}

func appendSlugSuffix(slug string, suffix string, maxLen int) string {
	if suffix == "" {
		return truncateRunes(slug, maxLen)
	}

	delimiter := "-"
	slugRunes := []rune(slug)
	suffixRunes := []rune(suffix)
	available := maxLen - len(delimiter) - len(suffixRunes)

	if available < 0 {
		available = 0
	}

	if len(slugRunes) > available {
		slugRunes = slugRunes[:available]
	}

	if len(slugRunes) == 0 {
		return truncateRunes(suffix, maxLen)
	}

	return fmt.Sprintf("%s%s%s", string(slugRunes), delimiter, suffix)
}

func truncateRunes(value string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}

	return string(runes[:maxLen])
}

func isInvalidFilenameRune(r rune) bool {
	switch r {
	case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
		return true
	}

	return unicode.IsControl(r)
}

func isReservedWindowsBasename(value string) bool {
	trimmed := strings.TrimRight(strings.ToLower(value), ". ")
	switch trimmed {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return true
	default:
		return false
	}
}
