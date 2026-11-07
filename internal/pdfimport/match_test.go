package pdfimport

import (
	"strings"
	"testing"
)

func TestFindMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name, source, quote, want string
		line, column              int
	}{
		{"formatting", "# Title\n\n日本語の**重要な説明**と[参照](https://example.com)です。\n", "重要な説明と参照", "重要な説明**と[参照", 3, 6},
		{"wrapped", "段落の最初です。\n次の文です。\n", "最初です。 次の文です。", "最初です。\n次の文です。", 1, 3},
		{"code", "`foo_bar && a < b`\n", "foo_bar && a < b", "foo_bar && a < b", 1, 1},
		{"entities", "A &amp; B \\*text\\*\n", "A & B *text*", "A &amp; B \\*text\\*", 1, 0},
		{"emoji BOM CRLF", "\ufeff# 見出し\r\n\r\n😀日本語の文章\r\n", "日本語の文章", "日本語の文章", 3, 1},
		{"frontmatter", "---\ntitle: 本文の文章\n---\n\n本文の文章\n", "本文の文章", "本文の文章", 5, 0},
		{"ligature", "The office is open.\n", "The oﬃce is open.", "The office is open.", 1, 0},
		{"fenced", "```go\nfoo_bar && a < b\n```\n", "foo_bar && a < b", "foo_bar && a < b", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, auto, _ := Find([]*Index{NewIndex("chapter.md", "hash", []byte(tc.source))}, Highlight{Quote: tc.quote})
			if len(cs) != 1 || !auto {
				t.Fatalf("matches=%+v auto=%v", cs, auto)
			}
			r := cs[0].Range
			if r.Text != tc.want || r.StartLine != tc.line || r.StartColumn != tc.column {
				t.Fatalf("range=%+v want %q at %d:%d", r, tc.want, tc.line, tc.column)
			}
		})
	}
}

func TestAmbiguityAndContext(t *testing.T) {
	indexes := []*Index{
		NewIndex("one.md", "1", []byte("This is the first context. Repeated text. One ending.")),
		NewIndex("two.md", "2", []byte("Completely different context. Repeated text. Two ending.")),
	}
	cs, auto, _ := Find(indexes, Highlight{Quote: "Repeated text."})
	if len(cs) != 2 || auto {
		t.Fatalf("ambiguity silently selected: %+v %v", cs, auto)
	}
	cs, auto, _ = Find(indexes, Highlight{Quote: "Repeated text.", Before: "This is the first context. "})
	if len(cs) != 2 || !auto || cs[0].Path != "one.md" {
		t.Fatalf("context match: %+v %v", cs, auto)
	}
	_, auto, truncated := Find([]*Index{NewIndex("many.md", "x", []byte(strings.Repeat("Repeated text.\n\n", 120)))}, Highlight{Quote: "Repeated text."})
	if auto || !truncated {
		t.Fatal("truncated results were auto-selected")
	}
}

func TestNegativeControls(t *testing.T) {
	x := NewIndex("chapter.md", "hash", []byte("First paragraph.\n\nSecond paragraph.\n\n[Shown](https://hidden.example.com)\n\n![hidden image text](x.png)\n"))
	for _, quote := range []string{"missing words", "paragraph.Second", "hidden.example.com", "hidden image text", "   ", "\x00", "\x00First"} {
		cs, auto, _ := Find([]*Index{x}, Highlight{Quote: quote})
		if len(cs) != 0 || auto {
			t.Fatalf("false positive %q: %+v", quote, cs)
		}
	}
	cs, auto, _ := Find([]*Index{x}, Highlight{Quote: "First paragraph.", Warning: "unsupported"})
	if len(cs) != 0 || auto {
		t.Fatal("unsupported extraction was matched")
	}
}
