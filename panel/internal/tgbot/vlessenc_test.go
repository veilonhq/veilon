package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

type encState struct {
	Flow     string `json:"flow"`
	VLESSEnc struct {
		Enabled       bool   `json:"enabled"`
		Auth          string `json:"auth"`
		Mode          string `json:"mode"`
		RTT           string `json:"rtt"`
		TicketSeconds int    `json:"ticket_seconds"`
		PrivateKey    string `json:"private_key"`
	} `json:"vless_encryption"`
}

func (r *rig) enc() encState {
	r.t.Helper()
	st, raw := r.srv.Do(context.Background(), r.root(), "GET", fmt.Sprintf("/inbounds/%d", r.ib), nil)
	if st != 200 {
		r.t.Fatalf("get inbound %d %s", st, raw)
	}
	var e encState
	_ = json.Unmarshal(raw, &e)
	return e
}

// The VLESS inbound screen shows VLESS Encryption; the bot turns it on
// (the server makes the key), switches key type, mode, 1-RTT, ticket time,
// makes a new key after a confirm, and toggles Vision.
func TestVLESSEncryptionFromBot(t *testing.T) {
	r := newRig(t)
	r.start()
	id := r.ib
	r.press(fmt.Sprintf("iv:%d", id), "رمزگذاری VLESS: خاموش")
	r.press(fmt.Sprintf("ve:%d", id), "وضعیت: خاموش")
	r.press(fmt.Sprintf("vet:%d", id), "X25519 · native · 0-RTT ۶۰۰s")
	e := r.enc()
	if !e.VLESSEnc.Enabled || e.VLESSEnc.PrivateKey == "" || e.Flow != "xtls-rprx-vision" {
		t.Fatalf("on %+v", e)
	}
	first := e.VLESSEnc.PrivateKey
	r.press(fmt.Sprintf("vea:%d:m", id), "ML-KEM-768 · native")
	if e = r.enc(); e.VLESSEnc.Auth != "mlkem768" || len(e.VLESSEnc.PrivateKey) != 86 {
		t.Fatalf("mlkem %+v", e.VLESSEnc)
	}
	r.press(fmt.Sprintf("vem:%d:xorpub", id), "ML-KEM-768 · xorpub · 0-RTT")
	r.press(fmt.Sprintf("ver:%d:1", id), "xorpub · 1-RTT")
	if e = r.enc(); e.VLESSEnc.RTT != "1rtt" || e.VLESSEnc.TicketSeconds != 0 {
		t.Fatalf("1rtt %+v", e.VLESSEnc)
	}
	r.press(fmt.Sprintf("ves:%d", id), "عمر بلیت را به ثانیه بفرستید")
	r.typeText("99999", "یک عدد از ۱ تا ۸۶۴۰۰ بفرستید")
	r.typeText("۹۰۰", "0-RTT ۹۰۰s")
	if e = r.enc(); e.VLESSEnc.RTT != "0rtt" || e.VLESSEnc.TicketSeconds != 900 {
		t.Fatalf("ticket %+v", e.VLESSEnc)
	}
	key := e.VLESSEnc.PrivateKey
	r.press(fmt.Sprintf("vek:%d", id), "لینک همه کاربران این ورودی عوض می شود")
	r.press(fmt.Sprintf("veky:%d", id), "وضعیت: ✅ روشن")
	if e = r.enc(); e.VLESSEnc.PrivateKey == key || e.VLESSEnc.PrivateKey == first {
		t.Fatal("new key not made")
	}
	r.press(fmt.Sprintf("vef:%d", id), "Flow (Vision): خاموش")
	if e = r.enc(); e.Flow != "" {
		t.Fatalf("vision off %+v", e)
	}
	r.press(fmt.Sprintf("vet:%d", id), "وضعیت: خاموش")
	if e = r.enc(); e.VLESSEnc.Enabled {
		t.Fatal("still on")
	}
}
