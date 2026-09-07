package trending

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var exactIntegerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*|[1-9][0-9]{0,2}(,[0-9]{3})+)$`)

func parsePage(body []byte, target *url.URL, period string) ([]Entry, error) {
	entries := []Entry{}
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return entries, fmt.Errorf("parse HTML: %w", err)
	}
	var rows []*html.Node
	challenge := false
	visit(document, func(node *html.Node) {
		if element(node, "title") || element(node, "h1") {
			value := strings.ToLower(nodeText(node))
			for _, phrase := range []string{"sign in", "verify you are human", "checking your browser", "just a moment", "access denied", "too many requests"} {
				if strings.Contains(value, phrase) {
					challenge = true
				}
			}
		}
		if element(node, "form") && attr(node, "action") == "/session" {
			challenge = true
		}
		if element(node, "article") && hasClass(node, "Box-row") {
			rows = append(rows, node)
		}
	})
	if challenge {
		return entries, errors.New("login or access-challenge page received instead of Trending")
	}
	if len(rows) == 0 {
		return entries, errors.New("no Trending repository rows found; page may be empty, blocked, or its layout changed")
	}
	if len(rows) > 100 {
		return entries, fmt.Errorf("unexpected Trending row count %d exceeds 100", len(rows))
	}
	seen := map[string]bool{}
	var failures []error
	for index, row := range rows {
		entry, err := parseEntry(row, target, period)
		entry.Rank = index + 1
		if err != nil {
			failures = append(failures, fmt.Errorf("row %d: %w", index+1, err))
		}
		if entry.FullName == "" {
			continue
		}
		key := strings.ToLower(entry.FullName)
		if seen[key] {
			failures = append(failures, fmt.Errorf("row %d: duplicate repository %s", index+1, entry.FullName))
			continue
		}
		seen[key] = true
		entries = append(entries, entry)
	}
	return entries, errors.Join(failures...)
}

func parseEntry(row *html.Node, target *url.URL, period string) (Entry, error) {
	entry := Entry{}
	var headings []*html.Node
	visit(row, func(node *html.Node) {
		if element(node, "h2") {
			headings = append(headings, node)
		}
	})
	if len(headings) != 1 {
		return entry, errors.New("expected one repository h2 heading")
	}
	var links []*html.Node
	visit(headings[0], func(node *html.Node) {
		if element(node, "a") {
			links = append(links, node)
		}
	})
	if len(links) != 1 {
		return entry, errors.New("expected one repository heading link")
	}
	reference, err := safeLink(target, attr(links[0], "href"))
	if err != nil {
		return entry, fmt.Errorf("invalid repository link: %w", err)
	}
	parts := strings.Split(strings.TrimPrefix(reference.Path, "/"), "/")
	if len(parts) != 2 || !ownerPattern.MatchString(parts[0]) || !repositoryPattern.MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." {
		return entry, errors.New("repository heading must link to owner/name")
	}
	fullName := strings.Join(parts, "/")
	if !strings.EqualFold(strings.Join(strings.Fields(nodeText(links[0])), ""), fullName) {
		return entry, errors.New("repository heading text does not match its link")
	}
	entry.FullName = fullName
	var totalLinks, periodSpans []*html.Node
	visit(row, func(node *html.Node) {
		if element(node, "a") {
			link, err := safeLink(target, attr(node, "href"))
			if err == nil && strings.EqualFold(link.Path, "/"+fullName+"/stargazers") {
				totalLinks = append(totalLinks, node)
			}
		}
		if element(node, "span") && hasClass(node, "float-sm-right") {
			periodSpans = append(periodSpans, node)
		}
	})
	var failures []error
	if len(totalLinks) != 1 {
		failures = append(failures, errors.New("missing or ambiguous cumulative Star link"))
	} else {
		value, err := parseExactInteger(nodeText(totalLinks[0]))
		if err != nil {
			failures = append(failures, fmt.Errorf("cumulative Stars: %w", err))
		} else {
			entry.TotalStars = &value
		}
	}
	if len(periodSpans) != 1 {
		failures = append(failures, errors.New("missing or ambiguous period Star count"))
	} else {
		value, err := parsePeriodStars(nodeText(periodSpans[0]), period)
		if err != nil {
			failures = append(failures, err)
		} else {
			entry.StarsInPeriod = &value
		}
	}
	return entry, errors.Join(failures...)
}

func parsePeriodStars(value, period string) (int64, error) {
	value = strings.Join(strings.Fields(value), " ")
	unit := map[string]string{"daily": "today", "weekly": "this week", "monthly": "this month"}[period]
	for _, suffix := range []string{" stars " + unit, " star " + unit} {
		if strings.HasSuffix(value, suffix) {
			return parseExactInteger(strings.TrimSuffix(value, suffix))
		}
	}
	return 0, fmt.Errorf("period Star count does not identify the requested %s window", period)
}

func parseExactInteger(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if !exactIntegerPattern.MatchString(value) {
		return 0, errors.New("expected an exact nonnegative integer, not an abbreviated or malformed number")
	}
	parsed, err := strconv.ParseInt(strings.ReplaceAll(value, ",", ""), 10, 64)
	if err != nil {
		return 0, errors.New("Star count exceeds the supported integer range")
	}
	return parsed, nil
}

func safeLink(base *url.URL, value string) (*url.URL, error) {
	reference, err := url.Parse(value)
	if err != nil || value == "" {
		return nil, errors.New("missing or malformed href")
	}
	resolved := base.ResolveReference(reference)
	if !sameOrigin(base, resolved) || resolved.User != nil || resolved.RawQuery != "" || resolved.Fragment != "" || resolved.RawPath != "" {
		return nil, errors.New("href is not a plain same-origin repository path")
	}
	return resolved, nil
}

func visit(node *html.Node, fn func(*html.Node)) {
	fn(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		visit(child, fn)
	}
}

func element(node *html.Node, name string) bool {
	return node.Type == html.ElementNode && node.Data == name
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func nodeText(node *html.Node) string {
	var text strings.Builder
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if element(node, "svg") || element(node, "script") || element(node, "style") {
			return
		}
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(node)
	return strings.TrimSpace(text.String())
}
