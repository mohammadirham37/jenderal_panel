package nginx

import (
	"bytes"
	"os"
)

const ipv6InterfacesPath = "/proc/net/if_inet6"

// IPv6Available reports whether the host has at least one IPv6 interface.
func IPv6Available() bool {
	return ipv6AvailableAt(ipv6InterfacesPath)
}

func ipv6AvailableAt(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && len(bytes.TrimSpace(data)) > 0
}
