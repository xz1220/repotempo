// Package auth implements restricted GitHub sign-in, separate from collection.
package auth

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrConfiguration   = errors.New("auth: invalid configuration")
	ErrInvalidState    = errors.New("auth: invalid or expired login state")
	ErrUnauthenticated = errors.New("auth: session missing or expired")
	ErrForbidden       = errors.New("auth: GitHub account not allowed")
	ErrProvider        = errors.New("auth: GitHub authentication unavailable")
	ErrStorage         = errors.New("auth: authentication storage unavailable")
	ErrCSRF            = errors.New("auth: invalid request verification")
	ErrInvalidReturn   = errors.New("auth: invalid return path")
)

type Configuration struct {
	ClientID       string
	ClientSecret   string `json:"-"`
	PublicURL      string
	AllowedUserIDs []int64
}

func (config Configuration) Validate() error {
	if config.ClientID == "" || config.ClientSecret == "" || len(config.ClientID) > 256 || len(config.ClientSecret) > 1024 ||
		strings.ContainsFunc(config.ClientID+config.ClientSecret, unicode.IsSpace) || strings.ContainsFunc(config.ClientID+config.ClientSecret, unicode.IsControl) || len(config.AllowedUserIDs) == 0 {
		return ErrConfiguration
	}
	if _, err := publicOrigin(config.PublicURL); err != nil {
		return err
	}
	for _, id := range config.AllowedUserIDs {
		if id <= 0 {
			return ErrConfiguration
		}
	}
	return nil
}

func publicOrigin(value string) (string, error) {
	if len(value) > 2048 || strings.ContainsAny(value, "\\%") || strings.ContainsFunc(value, unicode.IsSpace) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", ErrConfiguration
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", ErrConfiguration
	}
	host := parsed.Hostname()
	if host == "" || strings.HasPrefix(host, ".") || strings.Contains(host, "..") {
		return "", ErrConfiguration
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", ErrConfiguration
	}
	for _, char := range host {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune(".-:", char)) {
			return "", ErrConfiguration
		}
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", ErrConfiguration
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return "", ErrConfiguration
	}
	loopback := strings.EqualFold(host, "localhost")
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopback) {
		return "", ErrConfiguration
	}
	parsed.Host, parsed.Path = strings.ToLower(parsed.Host), ""
	if parsed.Scheme == "https" && parsed.Port() == "443" || parsed.Scheme == "http" && parsed.Port() == "80" {
		parsed.Host = strings.ToLower(host)
		if strings.Contains(host, ":") {
			parsed.Host = "[" + parsed.Host + "]"
		}
	}
	return parsed.String(), nil
}
