package main

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// listenHost maps a --bind value to the host given to net.Listen. "all"
// listens on every interface over both IPv4 and IPv6, which matters for
// mDNS names that also resolve to IPv6 addresses.
func listenHost(bind string) string {
	if bind == "all" {
		return ""
	}
	return bind
}

func isWildcard(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::"
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// mdnsName returns the name under which this machine is reachable by mDNS.
func mdnsName(hostname string) string {
	h := strings.TrimSuffix(hostname, ".")
	if h == "" || strings.Contains(h, ".") {
		return h
	}
	return h + ".local"
}

// accessURLs lists URLs that open the UI, the preferred one first. For a
// wildcard listener the mDNS host name comes first, followed by the
// addresses of the network interfaces.
func accessURLs(host string, port int, token string, hostname string, addrs []net.Addr) []string {
	mk := func(h string) string {
		u := url.URL{Scheme: "http", Host: net.JoinHostPort(h, strconv.Itoa(port)), Path: "/", RawQuery: "token=" + url.QueryEscape(token)}
		return u.String()
	}
	if !isWildcard(host) {
		return []string{mk(host)}
	}
	var out []string
	if n := mdnsName(hostname); n != "" {
		out = append(out, mk(n))
	}
	var v6 []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		if ip4 := ipn.IP.To4(); ip4 != nil {
			out = append(out, mk(ip4.String()))
		} else if host != "0.0.0.0" {
			v6 = append(v6, mk(ipn.IP.String()))
		}
	}
	out = append(out, v6...)
	if len(out) == 0 {
		out = append(out, mk("127.0.0.1"))
	}
	return out
}

func baseURL(public string, urls []string) string {
	if public != "" {
		return strings.TrimRight(public, "/")
	}
	if len(urls) == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.SplitN(urls[0], "?", 2)[0], "/")
}

func preferPublicURL(public, token string, urls []string) []string {
	if public == "" {
		return urls
	}
	u := baseURL(public, nil) + "/?token=" + url.QueryEscape(token)
	return append([]string{u}, urls...)
}

// virtualIfacePrefixes are container and VM bridges whose addresses are not
// reachable from other machines.
var virtualIfacePrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "podman", "lxc", "vmnet"}

func interfaceAddrs() []net.Addr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Addr
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || isVirtualIface(ifc.Name) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		out = append(out, addrs...)
	}
	return out
}

func isVirtualIface(name string) bool {
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
