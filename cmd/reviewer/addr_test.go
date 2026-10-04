package main

import (
	"net"
	"reflect"
	"testing"
)

func TestMDNSName(t *testing.T) {
	for in, want := range map[string]string{
		"devbox":             "devbox.local",
		"devbox.local":       "devbox.local",
		"devbox.local.":      "devbox.local",
		"devbox.example.com": "devbox.example.com",
		"":                   "",
	} {
		if got := mdnsName(in); got != want {
			t.Errorf("mdnsName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAccessURLs(t *testing.T) {
	ipnet := func(s string) net.Addr {
		ip, n, _ := net.ParseCIDR(s)
		n.IP = ip
		return n
	}
	addrs := []net.Addr{ipnet("127.0.0.1/8"), ipnet("192.168.1.10/24"), ipnet("fe80::1/64"), ipnet("2001:db8::10/64")}

	got := accessURLs(listenHost("all"), 7777, "tok", "devbox", addrs)
	want := []string{
		"http://devbox.local:7777/?token=tok",
		"http://192.168.1.10:7777/?token=tok",
		"http://[2001:db8::10]:7777/?token=tok",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("all = %q", got)
	}
	if got := accessURLs("0.0.0.0", 7777, "tok", "devbox", addrs); len(got) != 2 {
		t.Errorf("0.0.0.0 should skip IPv6: %q", got)
	}
	if got := accessURLs("127.0.0.1", 7777, "tok", "devbox", addrs); !reflect.DeepEqual(got, []string{"http://127.0.0.1:7777/?token=tok"}) {
		t.Errorf("loopback = %q", got)
	}
}

func TestBaseURLPrefersPublic(t *testing.T) {
	urls := []string{"http://devbox.local:7777/?token=secret", "http://192.168.1.10:7777/?token=secret"}
	for _, tc := range []struct {
		name, public string
		urls         []string
		want         string
	}{
		{"public", "https://devbox.example.ts.net", urls, "https://devbox.example.ts.net"},
		{"unset", "", urls, "http://devbox.local:7777"},
		{"trailing slash", "https://devbox.example.ts.net/", urls, "https://devbox.example.ts.net"},
		{"trailing slashes", "https://devbox.example.ts.net///", urls, "https://devbox.example.ts.net"},
		{"public only", "https://devbox.example.ts.net", nil, "https://devbox.example.ts.net"},
		{"empty", "", nil, ""},
		{"loopback", "", []string{"http://127.0.0.1:7777/?token=secret"}, "http://127.0.0.1:7777"},
		{"IPv6", "", []string{"http://[::1]:7777/?token=secret"}, "http://[::1]:7777"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := baseURL(tc.public, tc.urls); got != tc.want {
				t.Errorf("baseURL(%q, %q) = %q, want %q", tc.public, tc.urls, got, tc.want)
			}
		})
	}
}

func TestPreferPublicURL(t *testing.T) {
	urls := []string{"http://devbox.local:7777/?token=a%2Bb+%26%3F", "http://192.168.1.10:7777/?token=a%2Bb+%26%3F"}
	if got := preferPublicURL("", "a+b &?", urls); !reflect.DeepEqual(got, urls) {
		t.Errorf("unset public URL = %q, want %q", got, urls)
	}
	want := append([]string{"https://devbox.example.ts.net/?token=a%2Bb+%26%3F"}, urls...)
	got := preferPublicURL("https://devbox.example.ts.net///", "a+b &?", urls)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("public URL = %q, want %q", got, want)
	}
	if got := withPath(got[0], "/p/project-id/"); got != "https://devbox.example.ts.net/p/project-id/?token=a%2Bb+%26%3F" {
		t.Errorf("project access URL = %q", got)
	}
}

func TestIsVirtualIface(t *testing.T) {
	for name, want := range map[string]bool{"docker0": true, "br-1a2b": true, "veth12": true, "eth0": false, "tailscale0": false, "en0": false} {
		if got := isVirtualIface(name); got != want {
			t.Errorf("isVirtualIface(%q) = %v", name, got)
		}
	}
}
