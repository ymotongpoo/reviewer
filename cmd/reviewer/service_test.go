package main

import (
	"strings"
	"testing"
)

func TestUnitFile(t *testing.T) {
	u := unitFile("/home/me/.local/bin/reviewer", []string{"serve", "--port", "7777"})
	for _, want := range []string{"ExecStart=/home/me/.local/bin/reviewer serve --port 7777", "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
	if u := unitFile("/opt/my apps/reviewer", []string{"serve"}); !strings.Contains(u, `ExecStart="/opt/my apps/reviewer" serve`) {
		t.Errorf("path with space not quoted:\n%s", u)
	}
}
