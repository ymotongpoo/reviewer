package gitops

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Languages of generated commit messages.
const (
	LangJA = "ja"
	LangEN = "en"
)

// ValidLanguage reports whether lang is a supported message language.
func ValidLanguage(lang string) bool { return lang == LangJA || lang == LangEN }

// maxSubject keeps the generated header within the usual 72 columns.
const maxSubject = 72

// maxBodyFiles bounds the file list in a generated body.
const maxBodyFiles = 30

// Message generates a Conventional Commits message for changes.
func Message(changes []Change, lang string) string {
	if len(changes) == 0 {
		return ""
	}
	typ := commitType(changes)
	scope := commitScope(changes)
	header := typ
	if scope != "" {
		header += "(" + scope + ")"
	}
	header += ": "
	desc := description(changes, lang, maxSubject-utf8.RuneCountInString(header))
	msg := header + desc
	if len(changes) > 1 || changes[0].Kind == Renamed {
		msg += "\n\n" + body(changes, lang)
	}
	return msg + "\n"
}

var docExt = map[string]bool{
	".md": true, ".mdx": true, ".markdown": true, ".adoc": true, ".asciidoc": true,
	".rst": true, ".txt": true, ".tex": true, ".org": true, ".textile": true,
}

var buildFiles = map[string]bool{
	"makefile": true, "dockerfile": true, "go.mod": true, "go.sum": true,
	"package.json": true, "package-lock.json": true, "pnpm-lock.yaml": true,
	"yarn.lock": true, "cargo.toml": true, "cargo.lock": true, "pyproject.toml": true,
	"requirements.txt": true, "gemfile": true, "gemfile.lock": true,
	"build.gradle": true, "pom.xml": true, "cmakelists.txt": true,
}

func category(p string) string {
	base := strings.ToLower(path.Base(p))
	lower := strings.ToLower(p)
	switch {
	case strings.HasPrefix(lower, ".github/workflows/") || strings.Contains(lower, "/.github/workflows/") ||
		base == ".gitlab-ci.yml" || strings.HasPrefix(lower, ".circleci/") || base == "cloudbuild.yaml":
		return "ci"
	case buildFiles[base]:
		return "build"
	case strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(lower, "test/") || strings.HasPrefix(lower, "tests/") ||
		strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") || strings.Contains(lower, "/testdata/"):
		return "test"
	case docExt[path.Ext(base)] || strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "changelog") ||
		strings.HasPrefix(base, "license") || strings.HasPrefix(lower, "docs/") || strings.Contains(lower, "/docs/"):
		return "docs"
	case strings.HasPrefix(base, ".") || path.Ext(base) == "" || isConfigExt(path.Ext(base)):
		return "chore"
	default:
		return "code"
	}
}

func isConfigExt(ext string) bool {
	switch ext {
	case ".yaml", ".yml", ".toml", ".json", ".ini", ".cfg", ".conf", ".lock":
		return true
	}
	return false
}

// commitType picks the type every changed file agrees on; mixed changes
// fall back to the most significant one.
func commitType(changes []Change) string {
	seen := map[string]bool{}
	newCode := false
	for _, c := range changes {
		cat := category(c.Path)
		seen[cat] = true
		if cat == "code" && c.Kind == Added {
			newCode = true
		}
	}
	if len(seen) == 1 {
		for cat := range seen {
			if cat != "code" {
				return cat
			}
		}
	}
	switch {
	case newCode:
		return "feat"
	case seen["code"]:
		return "fix"
	case seen["docs"]:
		return "docs"
	case seen["test"]:
		return "test"
	case seen["build"]:
		return "build"
	case seen["ci"]:
		return "ci"
	default:
		return "chore"
	}
}

var scopeUnsafe = regexp.MustCompile(`[^\p{L}\p{N}._/-]+`)

// commitScope is the top-level directory shared by every change.
func commitScope(changes []Change) string {
	first := ""
	for i, c := range changes {
		dir, _, nested := strings.Cut(c.Path, "/")
		if !nested {
			dir = ""
		}
		if i == 0 {
			first = dir
		} else if dir != first {
			first = ""
			break
		}
	}
	first = strings.Trim(scopeUnsafe.ReplaceAllString(first, "-"), "-.")
	if utf8.RuneCountInString(first) > 24 {
		return ""
	}
	return first
}

func verb(kind, lang string) string {
	if lang == LangJA {
		switch kind {
		case Added:
			return "追加"
		case Deleted:
			return "削除"
		case Renamed:
			return "名前を変更"
		default:
			return "更新"
		}
	}
	switch kind {
	case Added:
		return "add"
	case Deleted:
		return "remove"
	case Renamed:
		return "rename"
	default:
		return "update"
	}
}

// description summarizes the changes in at most limit characters: file
// names when they fit, a count otherwise.
func description(changes []Change, lang string, limit int) string {
	kind := changes[0].Kind
	for _, c := range changes[1:] {
		if c.Kind != kind {
			kind = Modified
			break
		}
	}
	if kind == Conflict || kind == TypeChange {
		kind = Modified
	}
	if len(changes) == 1 && kind == Renamed {
		c := changes[0]
		var s string
		if lang == LangJA {
			s = fmt.Sprintf("%s の名前を %s に変更", path.Base(c.From), path.Base(c.Path))
		} else {
			s = fmt.Sprintf("rename %s to %s", path.Base(c.From), path.Base(c.Path))
		}
		if utf8.RuneCountInString(s) <= limit {
			return s
		}
	}
	if len(changes) <= 3 {
		names := make([]string, len(changes))
		for i, c := range changes {
			names[i] = path.Base(c.Path)
		}
		var s string
		if lang == LangJA {
			obj := strings.Join(names, "、")
			if kind == Renamed {
				s = obj + " の名前を変更"
			} else {
				s = obj + " を" + verb(kind, lang)
			}
		} else {
			obj := names[0]
			if len(names) > 1 {
				obj = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
			}
			s = verb(kind, lang) + " " + obj
		}
		if utf8.RuneCountInString(s) <= limit {
			return s
		}
	}
	if lang == LangJA {
		if kind == Renamed {
			return fmt.Sprintf("%d件のファイルの名前を変更", len(changes))
		}
		return fmt.Sprintf("%d件のファイルを%s", len(changes), verb(kind, lang))
	}
	noun := "files"
	if len(changes) == 1 {
		noun = "file"
	}
	return fmt.Sprintf("%s %d %s", verb(kind, lang), len(changes), noun)
}

func body(changes []Change, lang string) string {
	var b strings.Builder
	for i, c := range changes {
		if i == maxBodyFiles {
			if lang == LangJA {
				fmt.Fprintf(&b, "- ほか%d件\n", len(changes)-i)
			} else {
				fmt.Fprintf(&b, "- and %d more\n", len(changes)-i)
			}
			break
		}
		kind := c.Kind
		if kind == Conflict || kind == TypeChange {
			kind = Modified
		}
		switch {
		case kind == Renamed && lang == LangJA:
			fmt.Fprintf(&b, "- %s の名前を %s に変更\n", c.From, c.Path)
		case kind == Renamed:
			fmt.Fprintf(&b, "- rename %s to %s\n", c.From, c.Path)
		case lang == LangJA:
			fmt.Fprintf(&b, "- %s を%s\n", c.Path, verb(kind, lang))
		default:
			fmt.Fprintf(&b, "- %s %s\n", verb(kind, lang), c.Path)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// MaxMessage bounds a commit message accepted from the UI.
const MaxMessage = 64 << 10

var headerRe = regexp.MustCompile(`^[a-z][a-z0-9-]*(\([^()\r\n]+\))?!?: \S`)

// ValidateMessage checks that message follows Conventional Commits: a
// "type(scope)!: description" header, then an optional body separated by a
// blank line. It returns the message with normalized line endings.
func ValidateMessage(message string) (string, error) {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.TrimSpace(message)
	switch {
	case message == "":
		return "", fmt.Errorf("コミットメッセージが空です")
	case len(message) > MaxMessage:
		return "", fmt.Errorf("コミットメッセージが長すぎます")
	case !utf8.ValidString(message) || strings.ContainsAny(message, "\x00\r"):
		return "", fmt.Errorf("コミットメッセージに使えない文字が含まれています")
	}
	header, rest, hasBody := strings.Cut(message, "\n")
	if !headerRe.MatchString(header) {
		return "", fmt.Errorf("1行目を Conventional Commits の形式（例: docs(ch1): 導入を短くする）にしてください")
	}
	if hasBody && !strings.HasPrefix(rest, "\n") {
		return "", fmt.Errorf("1行目と本文の間に空行を入れてください")
	}
	return message + "\n", nil
}
