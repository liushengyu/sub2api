package antigravity

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestBuildUserAgentCarriesHubFamilyAndPlatform(t *testing.T) {
	ua := BuildUserAgent("2.22.0")
	if want := "antigravity/hub/2.22.0 " + HubPlatform; ua != want {
		t.Fatalf("got %q, want %q", ua, want)
	}
	// An empty or malformed version must still produce a usable UA, never
	// "antigravity/hub/ darwin/arm64".
	for _, bad := range []string{"", "   ", "not-a-version", "2.22"} {
		ua := BuildUserAgent(bad)
		if !strings.HasPrefix(ua, "antigravity/hub/") || strings.Contains(ua, "hub/ ") {
			t.Fatalf("version %q produced %q", bad, ua)
		}
	}
}

func TestObfuscateSensitiveTextKeepsCasingAndSplitsMatches(t *testing.T) {
	words := []string{"Claude Code", "OpenClaw"}
	got := ObfuscateSensitiveText("Use CLAUDE CODE and OpenClaw here", words)
	for _, literal := range []string{"CLAUDE CODE", "OpenClaw"} {
		if strings.Contains(got, literal) {
			t.Fatalf("literal %q survived in %q", literal, got)
		}
	}
	if strings.Count(got, zeroWidthSpace) != 2 {
		t.Fatalf("expected one zero-width space per match, got %q", got)
	}
	// Stripping the marker must restore the original text exactly.
	if restored := strings.ReplaceAll(got, zeroWidthSpace, ""); restored != "Use CLAUDE CODE and OpenClaw here" {
		t.Fatalf("not reversible: %q", restored)
	}
	// Nothing configured means nothing changes.
	if out := ObfuscateSensitiveText("Use Claude Code", nil); out != "Use Claude Code" {
		t.Fatalf("unexpected rewrite with no words: %q", out)
	}
}

func TestObfuscateSystemInstructionLeavesOtherFieldsIntact(t *testing.T) {
	t.Setenv(AntigravitySensitiveWordsEnv, "Claude Code")
	// Reset the process-wide cache so the env var set above takes effect.
	sensitiveWordsOnce = sync.Once{}
	sensitiveWords = nil

	body := []byte(`{"systemInstruction":{"parts":[{"text":"You are Antigravity. Never say Claude Code."}]},` +
		`"contents":[{"role":"user","parts":[{"text":"Claude Code stays here"}]}]}`)
	out := ObfuscateSensitiveWordsInSystemInstruction(body)

	var parsed struct {
		SystemInstruction struct {
			Parts []struct{ Text string } `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Parts []struct{ Text string } `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if strings.Contains(parsed.SystemInstruction.Parts[0].Text, "Claude Code") {
		t.Fatalf("system instruction not obfuscated: %q", parsed.SystemInstruction.Parts[0].Text)
	}
	// The user's own message must be forwarded untouched.
	if parsed.Contents[0].Parts[0].Text != "Claude Code stays here" {
		t.Fatalf("user content was modified: %q", parsed.Contents[0].Parts[0].Text)
	}
}
