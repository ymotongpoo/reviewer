package gitops

import (
	"regexp"
	"strings"
)

// Masked replaces secrets in text shown to the user.
const Masked = "***"

type maskRule struct {
	re   *regexp.Regexp
	repl string
}

var maskRules = []maskRule{
	// Private key blocks, e.g. echoed by a misconfigured helper.
	{regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`), "-----BEGIN PRIVATE KEY----- " + Masked + " -----END PRIVATE KEY-----"},
	// Credentials in http(s) URLs: https://user:token@host, https://token@host.
	{regexp.MustCompile(`(?i)\b(https?|ftps?)://[^/\s@'"<>]+@`), "${1}://" + Masked + "@"},
	// Passwords in other URLs: ssh://user:password@host keeps the user.
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^/\s:@'"<>]*):[^/\s@'"<>]+@`), "${1}:" + Masked + "@"},
	// HTTP authorization headers and bearer tokens.
	{regexp.MustCompile(`(?i)\b((?:proxy-)?authorization:\s*)(?:(?:basic|bearer|token|digest|negotiate)\s+)?\S+`), "${1}" + Masked},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), "Bearer " + Masked},
	// key=value and key: value secrets (query strings, helper output).
	{regexp.MustCompile(`(?i)\b((?:access_?|private_?|auth_?|api_?|oauth_?)?token|password|passwd|pwd|secret|client_secret|api_?key)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&;,'"]+)`), "${1}${2}" + Masked},
	// Well-known token formats.
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`), Masked},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`), Masked},
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`), Masked},
	{regexp.MustCompile(`\bgl(?:dt|rt|soat|cbt|ptt|ft|imt|agent)-[A-Za-z0-9_-]{20,}`), Masked},
	{regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), Masked},
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), Masked},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), Masked},
	{regexp.MustCompile(`\bglsa_[A-Za-z0-9_]{20,}`), Masked},
	{regexp.MustCompile(`\bglc_[A-Za-z0-9+/=_-]{20,}`), Masked},
}

// Mask removes credentials from text produced by git (stderr, remote URLs)
// before it is logged or shown in the UI.
func Mask(s string) string {
	if s == "" {
		return s
	}
	for _, r := range maskRules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return strings.ToValidUTF8(s, "�")
}
