package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type advState struct {
	Advanced struct {
		Sniffing struct {
			RouteOnly bool `json:"route_only"`
		} `json:"sniffing"`
		Sockopt struct {
			ProxyProtocol bool `json:"accept_proxy_protocol"`
			TFO           bool `json:"tcp_fast_open"`
		} `json:"sockopt"`
		Reality struct {
			Seed   string `json:"mldsa65_seed"`
			Verify string `json:"mldsa65_verify"`
		} `json:"reality"`
		Reverse struct {
			Enabled bool   `json:"enabled"`
			UUID    string `json:"uuid"`
		} `json:"reverse"`
		SS *struct {
			NoUDP bool `json:"no_udp"`
		} `json:"ss"`
	} `json:"advanced"`
}

func (r *rig) adv(id int64) advState {
	r.t.Helper()
	st, raw := r.srv.Do(context.Background(), r.root(), "GET", fmt.Sprintf("/inbounds/%d", id), nil)
	if st != 200 {
		r.t.Fatalf("get inbound %d %s", st, raw)
	}
	var a advState
	_ = json.Unmarshal(raw, &a)
	return a
}

// The advanced screen of a VLESS Reality inbound: sniffing, socket switches,
// ML-DSA-65 on (the server makes the key) and off, the reverse proxy with
// the bridge config sent as a file; the inbound screen lists what is set.
func TestAdvancedFromBot(t *testing.T) {
	r := newRig(t)
	r.start()
	id := r.ib
	if c := r.press(fmt.Sprintf("iv:%d", id), "رمزگذاری VLESS"); !hasButton(c, "تنظیمات پیشرفته") {
		t.Fatal("no advanced button")
	}
	if c := r.press(fmt.Sprintf("xv:%d", id), "در حال استفاده: هیچ"); !hasButton(c, "PROXY protocol") || !hasButton(c, "ML-DSA-65") || hasButton(c, "ECH") {
		t.Fatalf("buttons %v", c.p["reply_markup"])
	}
	r.press(fmt.Sprintf("xs:%d:ro", id), "در حال استفاده: شنود")
	if a := r.adv(id); !a.Advanced.Sniffing.RouteOnly {
		t.Fatalf("route only %+v", a)
	}
	r.press(fmt.Sprintf("xs:%d:pp", id), "شنود، سوکت")
	r.press(fmt.Sprintf("xs:%d:tfo", id), "سوکت")
	if a := r.adv(id); !a.Advanced.Sockopt.ProxyProtocol || !a.Advanced.Sockopt.TFO || !a.Advanced.Sniffing.RouteOnly {
		t.Fatalf("sockopt %+v", a)
	}
	r.press(fmt.Sprintf("xs:%d:ml", id), "ML-DSA-65 روشن است")
	a := r.adv(id)
	if len(a.Advanced.Reality.Seed) != 43 || len(a.Advanced.Reality.Verify) < 2000 {
		t.Fatalf("mldsa %d %d", len(a.Advanced.Reality.Seed), len(a.Advanced.Reality.Verify))
	}
	c := r.press(fmt.Sprintf("xs:%d:ml", id), "در حال استفاده: شنود، سوکت\n")
	if strings.Contains(fmt.Sprint(c.p["text"]), "ML-DSA-65 روشن است") {
		t.Fatal("ml-dsa still shown as on")
	}
	if a = r.adv(id); a.Advanced.Reality.Seed != "" {
		t.Fatal("ml-dsa not off")
	}
	if c := r.press(fmt.Sprintf("xs:%d:rv", id), "از طریق سرور پل"); !hasButton(c, "فایل تنظیمات پل") {
		t.Fatal("no bridge button")
	}
	if a = r.adv(id); !a.Advanced.Reverse.Enabled || len(a.Advanced.Reverse.UUID) != 36 {
		t.Fatalf("reverse %+v", a.Advanced.Reverse)
	}
	n := r.tg.mark()
	r.tg.press(adminID, r.menu, fmt.Sprintf("xb:%d", id))
	doc := r.document(n)
	data, _ := doc.p["_data"].(string)
	if !strings.Contains(data, a.Advanced.Reverse.UUID) || !strings.Contains(data, "4443") || !strings.Contains(data, "finalRules") {
		t.Fatalf("bridge config %.300s", data)
	}
	r.press(fmt.Sprintf("iv:%d", id), "پیشرفته: شنود، سوکت، پروکسی معکوس")
	r.press(fmt.Sprintf("xs:%d:rv", id), "در حال استفاده")
	if a = r.adv(id); a.Advanced.Reverse.Enabled {
		t.Fatal("reverse still on")
	}
}

// Shadowsocks on RAW: the UDP switch.
func TestAdvancedSSUDPFromBot(t *testing.T) {
	r := newRig(t)
	st, raw := r.srv.Do(context.Background(), r.root(), "POST", "/inbounds", map[string]any{"remark": "SS", "protocol": "shadowsocks", "port": 4500, "ss_method": "aes-256-gcm"})
	if st != 201 {
		t.Fatalf("ss %d %s", st, raw)
	}
	var ib struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(raw, &ib)
	r.start()
	if c := r.press(fmt.Sprintf("xv:%d", ib.ID), "در حال استفاده: هیچ"); !hasButton(c, "UDP روی این پورت") {
		t.Fatalf("buttons %v", c.p["reply_markup"])
	}
	r.press(fmt.Sprintf("xs:%d:udp", ib.ID), "UDP خاموش")
	if a := r.adv(ib.ID); a.Advanced.SS == nil || !a.Advanced.SS.NoUDP {
		t.Fatalf("udp %+v", a.Advanced.SS)
	}
	r.press(fmt.Sprintf("xs:%d:udp", ib.ID), "در حال استفاده: هیچ")
}

func hasButton(c call, text string) bool {
	b, _ := json.Marshal(c.p["reply_markup"])
	var s string
	_ = json.Unmarshal(b, &s) // the markup may arrive as a JSON string
	return strings.Contains(string(b), text) || strings.Contains(s, text)
}
