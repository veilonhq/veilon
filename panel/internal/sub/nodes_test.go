package sub

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// Every node serving an inbound adds one link with the node's address; the
// server's own link stays as it was.
func TestNodesAddLinks(t *testing.T) {
	ibs := fixtures()
	plain := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "1.2.3.4"}, "x", Extras{})
	ex := Extras{Nodes: map[int64][]inbounds.NodeTarget{2: {{ID: 1, Name: "DE", Address: "de.example.com"}, {ID: 2, Name: "NL", Address: "5.6.7.8"}}}}
	ps := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "1.2.3.4"}, "x", ex)
	if len(ps) != len(plain)+2 {
		t.Fatalf("want %d proxies, got %d", len(plain)+2, len(ps))
	}
	var got []Proxy
	for _, p := range ps {
		if p.InboundID == 2 {
			got = append(got, p)
		}
	}
	if len(got) != 3 || got[0].Server != "1.2.3.4" || got[1].Server != "de.example.com" || got[2].Server != "5.6.7.8" {
		t.Fatalf("node links %+v", got)
	}
	if got[1].Name != "VM WS TLS · DE" || got[1].Port != got[0].Port || got[1].SNI != got[0].SNI || got[1].Path != got[0].Path {
		t.Fatalf("node link must keep the inbound settings: %+v", got[1])
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(URI(&got[2]), "vmess://"))
	if !strings.Contains(string(raw), `"add":"5.6.7.8"`) || !strings.Contains(string(raw), "· NL") {
		t.Fatal("share link without the node address")
	}
}

// Node-only AmneziaWG and MTProto inbounds: the .conf endpoint and the
// tg:// link use the node's address, never the main server's.
func TestNodeOnlyEngineLinks(t *testing.T) {
	ibs, ex := extraFixtures(t)
	ex.Nodes = map[int64][]inbounds.NodeTarget{14: {{ID: 1, Name: "DE", Address: "203.0.113.50"}}, 19: {{ID: 1, Name: "DE", Address: "203.0.113.50"}}}
	ex.Placed = map[int64]bool{14: true, 19: true}
	ex.NodeOnly = map[int64]bool{14: true, 19: true}
	ps := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "srv.example.com"}, "h", ex)
	var awg, tg []Proxy
	for _, p := range ps {
		switch p.InboundID {
		case 14:
			awg = append(awg, p)
		case 19:
			tg = append(tg, p)
		}
	}
	if len(awg) != 1 || len(tg) != 1 {
		t.Fatalf("awg %d tg %d", len(awg), len(tg))
	}
	if _, body, ok := ConfFile(&awg[0]); !ok || !strings.Contains(body, "Endpoint = 203.0.113.50:51821") || strings.Contains(body, "srv.example.com") {
		t.Fatalf("awg conf:\n%s", body)
	}
	if u := URI(&tg[0]); !strings.Contains(u, "server=203.0.113.50") || !strings.Contains(u, "port=993") || strings.Contains(u, "srv.example.com") {
		t.Fatalf("tg link %s", u)
	}
	if awg[0].Name != "AWG · DE" {
		t.Fatalf("name %q", awg[0].Name)
	}
}
