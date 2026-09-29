package anchor

import (
	"strings"
	"testing"
)

func lines(s string) []string { return strings.Split(s, "\n") }

const base = `# Title
intro line
## Section A
alpha one
alpha two
## Section B
beta one
beta two
end`

func TestResolve(t *testing.T) {
	old := lines(base)
	a := New(old, 4, 5) // alpha one..two

	tests := []struct {
		name      string
		new       string
		wantState State
		wantStart int
		wantEnd   int
	}{
		{"unchanged", base, Exact, 4, 5},
		{"inserted above", "new line\n" + base, Moved, 5, 6},
		{"deleted above", strings.Replace(base, "intro line\n", "", 1), Moved, 3, 4},
		{"moved section", "# Title\nintro line\n## Section B\nbeta one\nbeta two\n## Section A\nalpha one\nalpha two\nend", Moved, 7, 8},
		{"rewritten slightly", strings.Replace(base, "alpha two", "alpha 2", 1), Fuzzy, 4, 5},
		{"removed", strings.Replace(base, "alpha one\nalpha two\n", "", 1), Outdated, 4, 5},
		{"completely replaced", strings.Replace(base, "alpha one\nalpha two", "zzzzzzzzzz\nqqqqqqqqqqqq", 1), Outdated, 4, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(a, old, 4, 5, lines(tt.new), 0)
			if got.State != tt.wantState || got.Start != tt.wantStart || got.End != tt.wantEnd {
				t.Errorf("Resolve = %+v, want %s %d-%d", got, tt.wantState, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestResolveDuplicatePrefersContext(t *testing.T) {
	old := lines("a\nb\nTODO\nc\nx\ny\nTODO\nz")
	a := New(old, 7, 7)
	newLines := lines("new\na\nb\nTODO\nc\nx\ny\nTODO\nz")
	got := Resolve(a, old, 7, 7, newLines, 0)
	if got.Start != 8 || got.State != Moved {
		t.Errorf("got %+v, want moved to 8", got)
	}
}

func TestResolveWithoutOldContent(t *testing.T) {
	old := lines(base)
	a := New(old, 7, 8)
	got := Resolve(a, nil, 7, 8, lines("x\n"+base), 0)
	if got.Start != 8 || got.End != 9 || got.State != Moved {
		t.Errorf("got %+v", got)
	}
}
