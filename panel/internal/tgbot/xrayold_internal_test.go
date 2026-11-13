package tgbot

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNodeTextXrayTooOld: a node whose Xray-core is too old for some
// inbounds says which ones and what they need, in both languages.
func TestNodeTextXrayTooOld(t *testing.T) {
	var n nodeRow
	raw := `{"id":3,"name":"de-1","address":"203.0.113.9","api_port":62050,"enabled":true,
		"state":{"status":"online","version":"v0.3.9","xray_version":"25.6.8","synced":true,
		"xray_too_old":[{"id":7,"remark":"PQ","needs":[{"feature":"Reality ML-DSA-65","min":"25.7.26"}]}]}}`
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	if b := nodeBadge(&n); b != "🟡" {
		t.Fatalf("badge %q", b)
	}
	txt := nodeText(&n, nil)
	txt = strings.NewReplacer("\u2066", "", "\u2069", "").Replace(txt)
	fa := localize(txt, langFa)
	if !strings.Contains(fa, "Xray-core این نود برای این ورودی ها قدیمی است") || !strings.Contains(fa, "PQ: Reality ML-DSA-65 ≥ 25.7.26") {
		t.Fatalf("fa: %s", fa)
	}
	if strings.ContainsRune(fa, '\u200c') {
		t.Fatalf("half-space in %q", fa)
	}
	en := localize(txt, langEn)
	if !strings.Contains(en, "too old for these inbounds") || !strings.Contains(en, "Update the node's Xray.") || rePersian.MatchString(en) {
		t.Fatalf("en: %s", en)
	}
}
