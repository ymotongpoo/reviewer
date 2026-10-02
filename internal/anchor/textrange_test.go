package anchor

import (
	"errors"
	"testing"
)

func TestNormalizeRange(t *testing.T) {
	ls := []string{"日本語😀abc", "", "end"}
	tests := []struct {
		name string
		in   TextRange
		want TextRange
		text string
		err  bool
	}{
		{name: "code points", in: TextRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}, want: TextRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}, text: "語😀a"},
		{name: "multi-line", in: TextRange{StartLine: 1, StartColumn: 6, EndLine: 3, EndColumn: 2}, want: TextRange{StartLine: 1, StartColumn: 6, EndLine: 3, EndColumn: 2}, text: "c\n\nen"},
		{name: "end at column 0", in: TextRange{StartLine: 1, StartColumn: 4, EndLine: 3, EndColumn: 0}, want: TextRange{StartLine: 1, StartColumn: 4, EndLine: 2, EndColumn: 0}, text: "abc\n"},
		{name: "line break only", in: TextRange{StartLine: 1, StartColumn: 7, EndLine: 2, EndColumn: 0}, want: TextRange{StartLine: 1, StartColumn: 7, EndLine: 1, EndColumn: 7}, text: ""},
		{name: "column past end", in: TextRange{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 8}, err: true},
		{name: "reversed", in: TextRange{StartLine: 1, StartColumn: 3, EndLine: 1, EndColumn: 2}, err: true},
		{name: "line past end", in: TextRange{StartLine: 3, StartColumn: 0, EndLine: 4, EndColumn: 0}, err: true},
		{name: "line 0", in: TextRange{StartLine: 0, StartColumn: 0, EndLine: 1, EndColumn: 0}, err: true},
		{name: "negative column", in: TextRange{StartLine: 1, StartColumn: -1, EndLine: 1, EndColumn: 1}, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeRange(ls, tt.in)
			if tt.err {
				if !errors.Is(err, ErrInvalidRange) {
					t.Fatalf("err = %v, want ErrInvalidRange", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !got.SamePosition(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if text := RangeText(ls, got); text != tt.text {
				t.Fatalf("text = %q, want %q", text, tt.text)
			}
		})
	}
}

func TestNormalizeRangeRejectsInvalidUTF8(t *testing.T) {
	// A lone surrogate encoded as in CESU-8/WTF-8 is not valid UTF-8.
	ls := []string{"ok", "a\xed\xa0\x80b"}
	if _, err := NormalizeRange(ls, TextRange{StartLine: 2, StartColumn: 0, EndLine: 2, EndColumn: 1}); err == nil {
		t.Fatal("range on invalid UTF-8 accepted")
	}
	if _, err := NormalizeRange(ls, TextRange{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 2}); err != nil {
		t.Fatalf("valid line rejected: %v", err)
	}
}

func TestTextLines(t *testing.T) {
	got := TextLines([]string{"\uFEFFab", "c"})
	if got[0] != "ab" || got[1] != "c" {
		t.Fatalf("got %q", got)
	}
	if in := []string{"ab"}; &TextLines(in)[0] != &in[0] {
		t.Fatal("lines without BOM copied")
	}
}

func TestWithContext(t *testing.T) {
	ls := []string{"one two", "three"}
	r := WithContext(ls, TextRange{StartLine: 1, StartColumn: 4, EndLine: 2, EndColumn: 2})
	if r.Text != "two\nth" || r.Before != "one " || r.After != "ree" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveRange(t *testing.T) {
	old := []string{"alpha beta gamma", "beta delta", "end"}
	orig := WithContext(old, TextRange{StartLine: 2, StartColumn: 0, EndLine: 2, EndColumn: 4})
	tests := []struct {
		name  string
		new   []string
		want  TextRange
		found bool
	}{
		{name: "unchanged", new: old, want: TextRange{StartLine: 2, StartColumn: 0, EndLine: 2, EndColumn: 4}, found: true},
		{name: "moved down", new: []string{"new", "alpha beta gamma", "beta delta", "end"}, want: TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 4}, found: true},
		{name: "moved within line", new: []string{"alpha gamma", "xx beta delta", "end"}, want: TextRange{StartLine: 2, StartColumn: 3, EndLine: 2, EndColumn: 7}, found: true},
		{name: "unique", new: []string{"x", "y beta"}, want: TextRange{StartLine: 2, StartColumn: 2, EndLine: 2, EndColumn: 6}, found: true},
		{name: "context decides", new: []string{"zzz", "alpha beta gamma", "beta delta", "end"}, want: TextRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 4}, found: true},
		{name: "gone", new: []string{"alpha gamma", "delta"}},
		{name: "ambiguous", new: []string{"q", "q", "q", "beta delta end", "beta delta end"}},
		{name: "kept at the last position", new: []string{"beta", "beta delta"}, want: TextRange{StartLine: 2, StartColumn: 0, EndLine: 2, EndColumn: 4}, found: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResolveRange(orig, orig, tt.new)
			if ok != tt.found {
				t.Fatalf("found = %v, want %v (%+v)", ok, tt.found, got)
			}
			if ok && !got.SamePosition(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if ok && RangeText(tt.new, got) != orig.Text {
				t.Fatalf("text at result = %q", RangeText(tt.new, got))
			}
		})
	}
}

func TestResolveRangeMultiLineAndWide(t *testing.T) {
	old := []string{"前置き", "😀日本語の文", "です。"}
	orig := WithContext(old, TextRange{StartLine: 2, StartColumn: 1, EndLine: 3, EndColumn: 2})
	if orig.Text != "日本語の文\nです" {
		t.Fatalf("text = %q", orig.Text)
	}
	got, ok := ResolveRange(orig, orig, []string{"追加", "前置き", "😀😀日本語の文", "です。"})
	want := TextRange{StartLine: 3, StartColumn: 2, EndLine: 4, EndColumn: 2}
	if !ok || !got.SamePosition(want) {
		t.Fatalf("got %+v ok=%v, want %+v", got, ok, want)
	}
}

func TestResolveRangeTiedContextIsOutdated(t *testing.T) {
	old := []string{"k", "x", "k"}
	orig := WithContext(old, TextRange{StartLine: 2, StartColumn: 0, EndLine: 2, EndColumn: 1})
	// The original position no longer holds the text and both copies look alike.
	if got, ok := ResolveRange(orig, orig, []string{"k", "k", "x", "k", "x", "k"}); ok {
		t.Fatalf("ambiguous match accepted: %+v", got)
	}
}
