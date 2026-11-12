package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// fakeXray is an Xray controller that keeps the template in memory.
type fakeXray struct {
	xray.Controller
	mu      sync.Mutex
	t       *xray.Template
	applied int
}

func (f *fakeXray) LoadTemplate() *xray.Template {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(f.t)
	t, _ := xray.ParseTemplate(b)
	return t
}

func (f *fakeXray) TestTemplate(_ context.Context, t *xray.Template) xray.TemplateResult {
	return xray.TemplateResult{OK: true, Tested: true, Warnings: []string{}}
}

func (f *fakeXray) ApplyTemplate(_ context.Context, t *xray.Template) (xray.TemplateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t
	f.applied++
	return xray.TemplateResult{OK: true, Tested: true, Applied: true, Warnings: []string{}}, nil
}

func (f *fakeXray) state() (*xray.Template, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t, f.applied
}

type xrayCore struct {
	fakeCore
	x *fakeXray
}

func (c xrayCore) XrayController() xray.Controller { return c.x }

func newXrayRig(t *testing.T) (*rig, *fakeXray) {
	tpl, err := xray.ParseTemplate([]byte(`{"outbounds":[
		{"tag":"warp","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":40000}]}},
		{"tag":"direct","protocol":"freedom"},{"tag":"blocked","protocol":"blackhole"}],
		"routing":{"rules":[
		{"type":"field","domain":["geosite:openai"],"outboundTag":"warp","_name":"AI"},
		{"type":"field","ip":["geoip:private"],"outboundTag":"blocked"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	fx := &fakeXray{t: tpl}
	r := newRigWith(t, func(srv *api.Server, _ *tgbot.Bot) { srv.Core = xrayCore{x: fx} })
	return r, fx
}

func TestXrayRuleDeleteNeedsConfirm(t *testing.T) {
	r, fx := newXrayRig(t)
	r.start()
	c := r.press("xr", "تنظیمات Xray")
	c = r.press(button(t, c, "قوانین"), "قوانین مسیریابی")
	c = r.press(button(t, c, "AI"), "قانون ۱ از ۲")
	c = r.press(button(t, c, "حذف"), "حذف شود")
	// "No" keeps the rule.
	c = r.press(button(t, c, "نه"), "قانون ۱ از ۲")
	c = r.press(button(t, c, "حذف"), "حذف شود")
	c = r.press(button(t, c, "بله"), "قوانین مسیریابی")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "۱ تغییر اعمال نشده") {
		t.Fatalf("draft state %v", c.p["text"])
	}
	// Nothing reaches Xray before apply.
	if tp, n := fx.state(); n != 0 || len(tp.Routing.Rules) != 2 {
		t.Fatalf("applied %d rules %d", n, len(tp.Routing.Rules))
	}
	c = r.press("xr", "تنظیمات Xray")
	c = r.press(button(t, c, "اعمال تغییرات"), "اعمال شود")
	if _, n := fx.state(); n != 0 {
		t.Fatal("applied before confirm")
	}
	r.press(button(t, c, "بله"), "تنظیمات Xray اعمال شد")
	tp, n := fx.state()
	if n != 1 || len(tp.Routing.Rules) != 1 || tp.Routing.Rules[0]["outboundTag"] != "blocked" {
		t.Fatalf("applied %d rules %v", n, tp.Routing.Rules)
	}
}

func TestXrayRuleAddMoveAndOutboundGuard(t *testing.T) {
	r, fx := newXrayRig(t)
	r.start()
	c := r.press("xnr", "نوع شرط")
	c = r.press(button(t, c, "دامنه"), "دامنه ها را بفرستید")
	c = r.typeText("ads.example.com، tracker.example.com", "به کجا برود")
	c = r.press(button(t, c, "blocked"), "قانون ۳ از ۳")
	c = r.press(button(t, c, "بالاتر"), "قانون ۲ از ۳")
	// The outbound used by a rule cannot be deleted.
	c = r.press("xo", "خروجی ها")
	c = r.press(button(t, c, "warp"), "127.0.0.1:40000")
	r.press(button(t, c, "حذف"), "حذف نمی شود")
	// An invalid rule edit is refused and leaves the draft as it was.
	r.press("xuj:0", "متن JSON کامل")
	r.typeText(`{"type":"field","outboundTag":"direct"}`, "پذیرفته نشد")
	c = r.press("xr", "تنظیمات Xray")
	c = r.press(button(t, c, "اعمال تغییرات"), "اعمال شود")
	r.press(button(t, c, "بله"), "اعمال شد")
	tp, _ := fx.state()
	if len(tp.Routing.Rules) != 3 {
		t.Fatalf("rules %v", tp.Routing.Rules)
	}
	got := tp.Routing.Rules[1]
	d, _ := json.Marshal(got["domain"])
	if got["outboundTag"] != "blocked" || string(d) != `["ads.example.com","tracker.example.com"]` {
		t.Fatalf("new rule %v", got)
	}
	if tp.Routing.Rules[0]["_name"] != "AI" {
		t.Fatalf("first rule %v", tp.Routing.Rules[0])
	}
}

func TestXrayOutboundFromLinkAndJSONFile(t *testing.T) {
	r, fx := newXrayRig(t)
	r.start()
	c := r.press("xon", "لینک اشتراک")
	r.typeText("trojan://pass@1.2.3.4:443?security=tls&sni=a.example.com#My Out", "My-Out")
	// Whole-template edit by file, with validation.
	n := r.tg.mark()
	r.tg.press(adminID, r.menu, "xj")
	r.tg.screen(n, "JSON کامل Xray")
	var doc map[string]any
	for _, m := range r.tg.since(n) {
		if m.method == "sendDocument" {
			_ = json.Unmarshal([]byte(m.p["_data"].(string)), &doc)
		}
	}
	if doc == nil {
		t.Fatal("template file not sent")
	}
	r.sendFile("bad.json", []byte(`{"outbounds":[{"tag":"x","protocol":"nope"}]}`), "پذیرفته نشد")
	r.tg.press(adminID, r.menu, "xj")
	r.tg.screen(n, "JSON کامل Xray")
	doc["routing"].(map[string]any)["domainStrategy"] = "IPIfNonMatch"
	b, _ := json.Marshal(doc)
	c = r.sendFile("good.json", b, "JSON بررسی شد")
	c = r.press(button(t, c, "اعمال تغییرات"), "اعمال شود")
	r.press(button(t, c, "بله"), "اعمال شد")
	tp, _ := fx.state()
	if tp.Routing.DomainStrategy != "IPIfNonMatch" || tp.Outbounds[1]["tag"] != "My-Out" || tp.Outbounds[1]["protocol"] != "trojan" {
		t.Fatalf("template %+v", tp)
	}
}
