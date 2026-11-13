package tgbot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Xray-core version of a node: the same choices as the main server's Xray
// card in the panel (the newest stable official releases plus the installed
// one), a confirmation, then the install's steps until the node reports the
// outcome. Agents without Xray installs get a note to update the node first.

func init() {
	on("nxr", nodeXrayPick)
	on("nxrp", nodeXrayAsk)
	on("nxry", nodeXrayStart)
	on("nxrw", nodeXrayWatch)
}

type xrayRelease struct {
	Tag string `json:"tag"`
}

// tagOf adds the "v" of a release tag.
func tagOf(v string) string {
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// cmpVer compares release tags (v26.10.31) numerically.
func cmpVer(a, b string) int {
	pa := strings.FieldsFunc(strings.TrimPrefix(a, "v"), func(r rune) bool { return r == '.' || r == '-' })
	pb := strings.FieldsFunc(strings.TrimPrefix(b, "v"), func(r rune) bool { return r == '.' || r == '-' })
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// xrayChoices is the picker's list: the newest five releases, plus the
// installed one when it is not among them, newest first.
func xrayChoices(rels []xrayRelease, current string) []string {
	var out []string
	for _, r := range rels {
		if len(out) == 5 {
			break
		}
		out = append(out, r.Tag)
	}
	has := false
	for _, t := range out {
		if t == current {
			has = true
		}
	}
	if current != "" && !has {
		out = append(out, current)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && cmpVer(out[j], out[j-1]) > 0; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func xrayRunning(phase string) bool {
	return phase != "" && phase != "done" && phase != "failed"
}

func nodeXrayPick(r *req, a []string) {
	id := argInt(a, 0)
	n, err := r.node(id)
	if err != nil {
		r.fail(err, "nl")
		return
	}
	if u := n.State.XrayUpdate; u != nil && xrayRunning(u.Phase) {
		nodeXrayWatch(r, a)
		return
	}
	t := header("🧩", T("Xray نود ")+n.Name)
	cur := tagOf(n.State.XrayVersion)
	if !n.State.CanUpdateXray {
		t += T("نسخه فعلی: ") + code(orDash(cur)) + "\n\n" + T("برنامه این نود نصب Xray از پنل را ندارد؛ اول نود را بروزرسانی کنید.")
		r.show(view{text: t, kb: Keyboard{nav(cbd("nv", id))}})
		return
	}
	var list struct {
		Releases []xrayRelease `json:"releases"`
	}
	if err := r.call("GET", "/core/xray/releases", nil, &list); err != nil {
		r.fail(err, cbd("nv", id))
		return
	}
	t += T("نسخه فعلی: ") + code(orDash(cur)) + "\n\n" + T("نسخه ای را که روی نود نصب شود انتخاب کنید (پنج نسخه پایدار آخر Xray-core):")
	kb := Keyboard{}
	for _, tag := range xrayChoices(list.Releases, cur) {
		if tag == cur {
			kb = append(kb, []Button{btn("✅ "+tag+T(" (فعلی)"), "nxr", id)})
			continue
		}
		kb = append(kb, []Button{btn("⬇️ "+tag, "nxrp", id, tag)})
	}
	kb = append(kb, nav(cbd("nv", id)))
	r.show(view{text: t, kb: kb})
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func nodeXrayAsk(r *req, a []string) {
	id, tag := argInt(a, 0), argStr(a, 1)
	n, err := r.node(id)
	if err != nil {
		r.fail(err, "nl")
		return
	}
	cur := tagOf(n.State.XrayVersion)
	q := "Xray " + code(tag) + T(" روی نود <b>") + esc(n.Name) + T("</b> نصب شود؟\n\nنود فایل رسمی این نسخه را دانلود و با checksum آن بررسی می کند، تنظیمات فعلی نود را با آن آزمایش می کند و بعد Xray نود چند ثانیه راه اندازی دوباره می شود. اگر نسخه جدید اجرا نشود، نسخه قبلی خودکار برمی گردد.")
	if cur != "" && cmpVer(tag, cur) < 0 {
		q += T("\n\n⚠️ این نسخه از نسخه فعلی قدیمی تر است؛ ورودی هایی که Xray جدیدتر می خواهند روی این نود کنار گذاشته می شوند.")
	}
	confirm(r, T("نصب Xray روی نود"), q, cbd("nxry", id, tag), cbd("nxr", id))
}

// nodeXrayErr is a refused install in the admin's language.
func nodeXrayErr(err error) string {
	var ae *apiErr
	if errors.As(err, &ae) {
		switch ae.Code {
		case "bad_version":
			return T("نسخه انتخاب شده معتبر نیست.")
		case "agent_unsupported":
			return T("برنامه این نود نصب Xray از پنل را ندارد؛ اول نود را بروزرسانی کنید.")
		case "offline":
			return T("نود در دسترس نیست.")
		case "up_to_date":
			return T("نود همین الان این نسخه را دارد.")
		case "in_progress":
			return T("نصب Xray روی این نود در حال انجام است.")
		case "rate_limited":
			return T("همین الان درخواست شد؛ چند ثانیه دیگر دوباره امتحان کنید.")
		}
	}
	return errText(err)
}

func nodeXrayStart(r *req, a []string) {
	id, tag := argInt(a, 0), argStr(a, 1)
	r.progress(T("در حال شروع نصب Xray روی نود"))
	if err := r.call("POST", fmt.Sprintf("/nodes/%d/xray", id), map[string]any{"version": tag}, nil); err != nil {
		r.show(view{text: header("⚠️", T("انجام نشد")) + esc(nodeXrayErr(err)), kb: Keyboard{{btn(T("🔙 بازگشت"), "nv", id), btn(T("🏠 خانه"), "home")}}})
		return
	}
	nodeXrayWatch(r, a)
}

// nodeXrayWatch follows a node's Xray install until it finishes.
func nodeXrayWatch(r *req, a []string) {
	id := argInt(a, 0)
	start := time.Now()
	r.watch(5*time.Minute, func(w *req) (view, bool) {
		n, err := w.node(id)
		if err != nil {
			return view{text: header("⚠️", T("انجام نشد")) + esc(errText(err)), kb: Keyboard{nav("nl")}}, true
		}
		u := n.State.XrayUpdate
		t := header("🧩", T("نصب Xray روی نود ")+n.Name)
		if u == nil {
			return view{text: t + T("اطلاعی از نصب Xray نیست."), kb: Keyboard{nav(cbd("nv", id))}}, true
		}
		t += T("از ") + code(orDash(tagOf(u.From))) + T(" به ") + code(u.Target) + "\n\n"
		steps := []string{"downloading", "verifying", "restarting"}
		cur := -1
		for i, s := range steps {
			if s == u.Phase {
				cur = i
			}
		}
		for i, s := range steps {
			mark := "▫️"
			switch {
			case u.Phase == "done" || i < cur:
				mark = "✅"
			case i == cur:
				mark = "⏳"
			}
			t += mark + " " + nodeXrayPhase[s] + "\n"
		}
		done := !xrayRunning(u.Phase)
		switch u.Phase {
		case "done":
			t += "\n✅ Xray " + code(u.Target) + T(" روی نود نصب شد و اجرا می شود.")
		case "failed":
			t += T("\n⚠️ نصب Xray انجام نشد: ") + esc(cut(u.Error, 300))
			if u.RolledBack {
				t += T("\nنسخه جدید اجرا نشد؛ Xray قبلی خودکار برگردانده شد و نود با همان کار می کند.")
			}
		default:
			t += "\n⏳ " + fa(strconv.Itoa(int(time.Since(start).Seconds()))) + T(" ثانیه · این صفحه خودکار تازه می شود.")
		}
		kb := Keyboard{}
		if !done {
			kb = append(kb, []Button{btn(T("🔄 تازه کردن"), "nxrw", id)})
		}
		kb = append(kb, nav(cbd("nv", id)))
		return view{text: t, kb: kb}, done
	})
}

var nodeXrayPhase = map[string]string{
	"downloading": T("دانلود و بررسی checksum"),
	"verifying":   T("آزمایش نسخه و تنظیمات نود"),
	"restarting":  T("راه اندازی دوباره Xray"),
}
