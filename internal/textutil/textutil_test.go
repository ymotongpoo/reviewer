package textutil

import (
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"a\n\nb", []string{"a", "", "b"}},
	}
	for _, tt := range tests {
		if got := SplitLines(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLineMap(t *testing.T) {
	a := []string{"1", "2", "3", "4", "5"}
	b := []string{"0", "1", "2x", "3", "5"}
	ops := LineDiff(a, b)
	m := BuildLineMap(ops, len(a), len(b))
	wantExact := []int{1, -1, 3, -1, 4}
	if !reflect.DeepEqual(m.Exact, wantExact) {
		t.Errorf("Exact = %v, want %v (ops %+v)", m.Exact, wantExact, ops)
	}
	if m.Near[1] != 2 {
		t.Errorf("Near[1] = %d, want 2", m.Near[1])
	}
	if m.Near[3] != 4 {
		t.Errorf("Near[3] = %d, want 4", m.Near[3])
	}
}

func TestSimilarity(t *testing.T) {
	if s := Similarity("abc", "abc"); s != 1 {
		t.Errorf("identical = %v", s)
	}
	if s := Similarity("abcdef", "abcxef"); s < 0.8 {
		t.Errorf("close = %v", s)
	}
	if s := Similarity("abc", "xyz"); s != 0 {
		t.Errorf("different = %v", s)
	}
}
