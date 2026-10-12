package antigravity

// Obfuscates configured words inside the system instruction before forwarding.
//
// Ported from CLIProxyAPI's oauth.providers.antigravity.sensitive-words. The
// upstream has been observed to rate-limit requests whose system prompt names
// third-party tooling; inserting a zero-width space inside those words keeps the
// text readable for the model while defeating exact string matching.
//
// Configure with ANTIGRAVITY_SENSITIVE_WORDS, a comma-separated list, e.g.
//   ANTIGRAVITY_SENSITIVE_WORDS=Claude Agent SDK,Claude Code,OpenClaw
// An empty or unset value disables the rewrite entirely.

import (
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// AntigravitySensitiveWordsEnv is the environment variable holding the word list.
const AntigravitySensitiveWordsEnv = "ANTIGRAVITY_SENSITIVE_WORDS"

const zeroWidthSpace = "​"

var (
	sensitiveWordsOnce sync.Once
	sensitiveWords     []string
)

// SensitiveWords returns the configured words, parsed once per process.
func SensitiveWords() []string {
	sensitiveWordsOnce.Do(func() {
		for _, raw := range strings.Split(os.Getenv(AntigravitySensitiveWordsEnv), ",") {
			if word := strings.TrimSpace(raw); word != "" {
				sensitiveWords = append(sensitiveWords, word)
			}
		}
	})
	return sensitiveWords
}

// obfuscateWord inserts a zero-width space after the first rune so the word no
// longer matches a literal comparison. Splitting once keeps the payload small;
// the model still reads the original word.
func obfuscateWord(word string) string {
	if word == "" {
		return word
	}
	_, size := utf8.DecodeRuneInString(word)
	return word[:size] + zeroWidthSpace + word[size:]
}

// ObfuscateSensitiveText rewrites every configured word found in text,
// case-insensitively, preserving the original casing of each match.
func ObfuscateSensitiveText(text string, words []string) string {
	if text == "" || len(words) == 0 {
		return text
	}
	lowerText := strings.ToLower(text)
	var out strings.Builder
	for i := 0; i < len(text); {
		matched := false
		for _, word := range words {
			n := len(word)
			if n == 0 || i+n > len(text) {
				continue
			}
			if lowerText[i:i+n] == strings.ToLower(word) {
				out.WriteString(obfuscateWord(text[i : i+n]))
				i += n
				matched = true
				break
			}
		}
		if !matched {
			out.WriteByte(text[i])
			i++
		}
	}
	return out.String()
}

// ObfuscateSensitiveWordsInSystemInstruction rewrites the configured words in a
// Gemini-shaped request body's systemInstruction parts. The body is returned
// unchanged when nothing is configured or nothing matches, so callers can use
// the result unconditionally.
func ObfuscateSensitiveWordsInSystemInstruction(body []byte) []byte {
	words := SensitiveWords()
	if len(words) == 0 || len(body) == 0 {
		return body
	}
	parts := gjson.GetBytes(body, "systemInstruction.parts")
	if !parts.Exists() || !parts.IsArray() {
		return body
	}
	out := body
	changed := false
	parts.ForEach(func(idx, part gjson.Result) bool {
		text := part.Get("text")
		if !text.Exists() {
			return true
		}
		rewritten := ObfuscateSensitiveText(text.String(), words)
		if rewritten == text.String() {
			return true
		}
		if updated, err := sjson.SetBytes(out, "systemInstruction.parts."+idx.String()+".text", rewritten); err == nil {
			out = updated
			changed = true
		}
		return true
	})
	if !changed {
		return body
	}
	return out
}
