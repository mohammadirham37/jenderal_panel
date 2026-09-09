package trafficguard

import (
	"strings"
	"testing"
)

func TestRenderRealIPTrustsHeaderOnlyFromConfiguredCIDR(t *testing.T) {
	got, err := RenderRealIP(WebsiteProfile{ProxyMode: "custom", ProxyHeader: "X-Forwarded-For", ProxyCIDRs: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "set_real_ip_from 192.0.2.0/24;") || !strings.Contains(got, "real_ip_header X-Forwarded-For;") {
		t.Fatalf("config=%s", got)
	}
}
func TestProxyRejectsLoopbackAndEmptyCustomCIDRs(t *testing.T) {
	for _, p := range []WebsiteProfile{{ProxyMode: "custom", ProxyHeader: "X-Real-IP"}, {ProxyMode: "custom", ProxyHeader: "X-Real-IP", ProxyCIDRs: []string{"127.0.0.0/8"}}} {
		if ValidateProxy(p) == nil {
			t.Fatalf("accepted %#v", p)
		}
	}
}
