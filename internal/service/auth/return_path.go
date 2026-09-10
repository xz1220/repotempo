package auth

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var numericDetail = regexp.MustCompile(`^/repositories/([1-9][0-9]*)$`)
var topicDetail = regexp.MustCompile(`^/topics/[a-z0-9][a-z0-9-]{0,99}$`)
var importDetail = regexp.MustCompile(`^/watch/imports/[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var projectAnchor = regexp.MustCompile(`^project-[1-9][0-9]*$`)

// SafeReturnPath accepts only navigable application pages and bounded, known
// query parameters. Encoded paths and nested redirects cannot change origin.
func SafeReturnPath(value string) (string, error) {
	if value == "" {
		return "/repositories", nil
	}
	return safeReturnPath(value, false)
}

func safeReturnPath(value string, nested bool) (string, error) {
	if len(value) > 8192 || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") || strings.ContainsFunc(value, unicode.IsControl) {
		return "", ErrInvalidReturn
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawPath != "" || strings.ContainsAny(parsed.Path, "%\\") || strings.ContainsFunc(parsed.Path, unicode.IsControl) {
		return "", ErrInvalidReturn
	}
	allowed := false
	switch parsed.Path {
	case "/", "/repositories", "/watch", "/watch/new", "/runs", "/topics", "/discoveries", "/account/api":
		allowed = true
	default:
		allowed = topicDetail.MatchString(parsed.Path) || importDetail.MatchString(parsed.Path)
		if match := numericDetail.FindStringSubmatch(parsed.Path); match != nil {
			_, err := strconv.ParseInt(match[1], 10, 64)
			allowed = err == nil
		}
	}
	if !allowed || nested && parsed.Path != "/repositories" || parsed.Fragment != "" && (parsed.Path != "/repositories" || !projectAnchor.MatchString(parsed.Fragment)) {
		return "", ErrInvalidReturn
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", ErrInvalidReturn
	}
	for key, values := range query {
		if parsed.Path == "/account/api" && key != "lang" {
			return "", ErrInvalidReturn
		}
		if len(values) != 1 || strings.Contains(values[0], "\\") || strings.ContainsFunc(values[0], unicode.IsControl) {
			return "", ErrInvalidReturn
		}
		switch key {
		case "date", "period", "sort", "tag", "topic", "source", "status", "q", "new", "focus", "view", "lang", "cursor", "page", "limit", "offset", "repository", "note":
		case "return_to":
			if nested || !numericDetail.MatchString(parsed.Path) {
				return "", ErrInvalidReturn
			}
			if _, err := safeReturnPath(values[0], true); err != nil {
				return "", err
			}
		default:
			return "", ErrInvalidReturn
		}
	}
	return parsed.String(), nil
}
