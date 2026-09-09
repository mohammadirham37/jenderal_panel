package trafficguard

import (
	"fmt"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"net/netip"
	"sort"
	"strings"
)

var allowedHeaders = map[string]bool{"X-Forwarded-For": true, "X-Real-IP": true, "CF-Connecting-IP": true}

func ValidateProxy(p WebsiteProfile) error {
	switch p.ProxyMode {
	case "direct":
		return nil
	case "cloudflare":
		if p.ProxyHeader != "" && p.ProxyHeader != "CF-Connecting-IP" {
			return model.NewValidationError("Cloudflare requires CF-Connecting-IP")
		}
		return nil
	case "custom":
		if !allowedHeaders[p.ProxyHeader] {
			return model.NewValidationError("unsupported trusted proxy header")
		}
		if len(p.ProxyCIDRs) == 0 {
			return model.NewValidationError("custom proxy requires trusted CIDRs")
		}
		_, err := normalizePrefixes(p.ProxyCIDRs)
		return err
	default:
		return model.NewValidationError("proxy mode must be direct, cloudflare, or custom")
	}
}
func normalizePrefixes(values []string) ([]netip.Prefix, error) {
	seen := map[string]bool{}
	var out []netip.Prefix
	for _, raw := range values {
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, model.NewValidationError("invalid trusted proxy CIDR")
		}
		p = p.Masked()
		a := p.Addr()
		if a.IsLoopback() || a.IsMulticast() || a.IsUnspecified() || a.IsLinkLocalUnicast() {
			return nil, model.NewValidationError("trusted proxy CIDR is not internet-routable")
		}
		if !seen[p.String()] {
			seen[p.String()] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}
func RenderRealIP(p WebsiteProfile, cloudflare ...netip.Prefix) (string, error) {
	if err := ValidateProxy(p); err != nil {
		return "", err
	}
	if p.ProxyMode == "direct" {
		return "", nil
	}
	var prefixes []netip.Prefix
	if p.ProxyMode == "cloudflare" {
		prefixes = cloudflare
		if len(prefixes) == 0 {
			return "", model.NewValidationError("Cloudflare CIDR snapshot is unavailable")
		}
		p.ProxyHeader = "CF-Connecting-IP"
	} else {
		var err error
		prefixes, err = normalizePrefixes(p.ProxyCIDRs)
		if err != nil {
			return "", err
		}
	}
	var b strings.Builder
	for _, prefix := range prefixes {
		fmt.Fprintf(&b, "set_real_ip_from %s;\n", prefix.String())
	}
	fmt.Fprintf(&b, "real_ip_header %s;\nreal_ip_recursive on;\n", p.ProxyHeader)
	return b.String(), nil
}
