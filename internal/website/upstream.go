package website

import (
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Upstream schemes accepted for reverse-proxy sites. Anything else could
// inject arbitrary nginx directives through the proxy_pass target.
var validProxySchemes = map[string]bool{"http": true, "https": true}

// hostnameRegex constrains upstream hosts to DNS label syntax; IP literals are
// accepted via net.ParseIP. Everything that is not a hostname or an IP is
// rejected so the value can never break out of the proxy_pass argument.
var hostnameRegex = regexp.MustCompile(
	`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$|^[a-zA-Z0-9]{1,63}$`)

// ProxyUpstream is the validated target of a reverse-proxy site.
type ProxyUpstream struct {
	Scheme string
	Host   string
	Port   int
}

// Target renders the upstream for nginx proxy_pass. IPv6 literals get
// bracketed ([::1]:3000) as required by nginx and RFC 3986.
func (u ProxyUpstream) Target() string {
	host := u.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host + ":" + strconv.Itoa(u.Port)
}

// ValidateUpstream checks a reverse-proxy upstream triple. Values end up in
// nginx configuration, so the rules are deliberately strict: scheme is
// http/https, host is a hostname or IP literal, port is 1-65535.
func ValidateUpstream(scheme, host string, port int) (ProxyUpstream, error) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	host = strings.ToLower(strings.TrimSpace(host))
	if !validProxySchemes[scheme] {
		return ProxyUpstream{}, model.NewValidationError("proxy scheme must be http or https")
	}
	if host == "" {
		return ProxyUpstream{}, model.NewValidationError("proxy host is required")
	}
	if len(host) > 253 {
		return ProxyUpstream{}, model.NewValidationError("proxy host is too long")
	}
	if net.ParseIP(host) == nil && !hostnameRegex.MatchString(host) {
		return ProxyUpstream{}, model.NewValidationError("proxy host must be a hostname or IP address")
	}
	if port < 1 || port > 65535 {
		return ProxyUpstream{}, model.NewValidationError("proxy port must be between 1 and 65535")
	}
	return ProxyUpstream{Scheme: scheme, Host: host, Port: port}, nil
}
