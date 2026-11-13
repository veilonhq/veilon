package tgbot

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const nodesPerPage = 8

// nodeBadge is the one-glance state of a node in lists.
func nodeBadge(n *nodeRow) string {
	switch {
	case !n.Enabled || n.State.Status == "disabled":
		return "⛔️"
	case n.State.Status == "online" && (n.UpdateTo != "" || len(n.State.NeedsUpdate) > 0):
		return "🟡"
	case n.State.Status == "online":
		return "🟢"
	case n.State.Status == "degraded":
		return "🟠"
	case n.State.Status == "waiting":
		return "⏳"
	default:
		return "🔴"
	}
}

func nodeStateText(n *nodeRow) string {
	switch {
	case !n.Enabled || n.State.Status == "disabled":
		return T("⛔️ خاموش")
	case n.State.Status == "online":
		return T("🟢 آنلاین")
	case n.State.Status == "degraded":
		return T("🟠 در دسترس، ولی Xray مشکل دارد")
	case n.State.Status == "waiting":
		return T("⏳ منتظر نصب (هنوز وصل نشده)")
	default:
		return T("🔴 آفلاین")
	}
}

var nodeUpdPhase = map[string]string{
	"downloading": T("دریافت نسخه تازه"),
	"verifying":   T("بررسی فایل"),
	"restarting":  T("راه اندازی دوباره"),
	"done":        T("تمام شد"),
	"failed":      T("ناموفق"),
}

func init() {
	on("nl", nodesList)
	on("nv", nodeView)
	on("nt", nodeToggle)
	on("nu", nodeUpdate)
	on("nw", nodeUpdateWatch)
	on("ni", func(r *req, a []string) {
		id := argInt(a, 0)
		n, err := r.node(id)
		if err != nil {
			r.fail(err, "nl")
			return
		}
		confirm(r, T("دستور نصب تازه"),
			T("برای نود <b>")+esc(n.Name)+T("</b> گواهی و دستور نصب تازه ساخته شود؟\n\n⚠️ گواهی فعلی نود باطل می شود و نود تا اجرای دستور تازه روی سرورش، وصل نمی شود."),
			cbd("niy", id), cbd("nv", id))
	})
	on("niy", func(r *req, a []string) {
		id := argInt(a, 0)
		r.progress(T("در حال ساختن دستور نصب"))
		var inst nodeInstall
		if err := r.call("POST", fmt.Sprintf("/nodes/%d/install", id), nil, &inst); err != nil {
			r.fail(err, cbd("nv", id))
			return
		}
		n, _ := r.node(id)
		name := ""
		if n != nil {
			name = n.Name
		}
		nodeShowInstall(r, id, name, &inst)
	})
	on("nd", func(r *req, a []string) {
		id := argInt(a, 0)
		n, err := r.node(id)
		if err != nil {
			r.fail(err, "nl")
			return
		}
		q := T("نود <b>") + esc(n.Name) + T("</b> حذف شود؟")
		if c := len(n.InboundIDs); c > 0 {
			q += "\n" + faInt(c) + T(" ورودی روی این نود است. ورودی ای که فقط روی این نود اجرا می شود به سرور اصلی برمی گردد (اگر پورتش روی سرور اصلی آزاد نباشد، حذف انجام نمی شود).")
		}
		q += T("\nاین کار برگشت ندارد.")
		confirm(r, T("حذف نود"), q, cbd("ndy", id), cbd("nv", id))
	})
	on("ndy", func(r *req, a []string) {
		id := argInt(a, 0)
		r.progress(T("در حال حذف نود"))
		if err := r.call("DELETE", fmt.Sprintf("/nodes/%d", id), nil, nil); err != nil {
			r.fail(err, cbd("nv", id))
			return
		}
		r.toast(T("🗑 حذف شد"))
		nodesList(r, nil)
	})
	// new node: name, address, agent port
	on("nn", func(r *req, _ []string) {
		r.st.draft = map[string]string{}
		r.ask("nnname", nil, T("نود جدید"), T("یک نام برای نود بفرستید (حداکثر ۴۸ حرف):"), "nl")
	})
	onInput("nnname", func(r *req, _ []string, text string, _ *Document) {
		r.st.draft["name"] = text
		r.ask("nnaddr", nil, T("نود جدید · ")+cut(text, 20), T("آدرس IP یا دامنه سرور نود را بفرستید (بدون http و پورت):"), "nl")
	})
	onInput("nnaddr", func(r *req, _ []string, text string, _ *Document) {
		r.st.draft["addr"] = text
		nodeAskPort(r, "")
	})
	on("nnp", func(r *req, a []string) {
		r.st.draft["port"] = argStr(a, 0)
		nodeCreate(r)
	})
	onInput("nnport", func(r *req, _ []string, text string, _ *Document) {
		n, ok := parseInt(text)
		if !ok || n < 1 || n > 65535 {
			nodeAskPort(r, T("⚠️ پورت باید عددی از ۱ تا ۶۵۵۳۵ باشد.\n"))
			return
		}
		r.st.draft["port"] = strconv.FormatInt(n, 10)
		nodeCreate(r)
	})
}

type nodeInstall struct {
	Command string `json:"command"`
	Port    int    `json:"port"`
}

func (r *req) node(id int64) (*nodeRow, error) {
	var ns []nodeRow
	if err := r.call("GET", "/nodes", nil, &ns); err != nil {
		return nil, err
	}
	for i := range ns {
		if ns[i].ID == id {
			return &ns[i], nil
		}
	}
	return nil, &apiErr{Status: 404, Message: "not found"}
}

func nodesList(r *req, a []string) {
	page := int(argInt(a, 0))
	if page < 1 {
		page = 1
	}
	var ns []nodeRow
	if err := r.call("GET", "/nodes", nil, &ns); err != nil {
		r.fail(err, "home")
		return
	}
	pages := (len(ns) + nodesPerPage - 1) / nodesPerPage
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	var on, off, upd int
	for i := range ns {
		switch {
		case !ns[i].Enabled:
		case ns[i].State.Status == "online":
			on++
		default:
			off++
		}
		if ns[i].UpdateTo != "" {
			upd++
		}
	}
	text := header("🌐", T("نود ها")) + T("تعداد: <b>") + faInt(len(ns)) + "</b>"
	if len(ns) > 0 {
		text += fmt.Sprintf("   🟢 %s   🔴 %s", faInt(on), faInt(off))
		if upd > 0 {
			text += "   ⬆️ " + faInt(upd)
		}
	}
	text += "\n"
	if len(ns) == 0 {
		text += T("\nهنوز نودی اضافه نشده است. با نود، ورودی ها روی سرور های دیگر هم اجرا می شوند.")
	} else {
		text += T("\n🟢 آنلاین · 🟡 نیاز به بروزرسانی · 🟠 مشکل Xray · 🔴 آفلاین · ⛔️ خاموش")
	}
	kb := Keyboard{}
	from := (page - 1) * nodesPerPage
	for i := from; i < len(ns) && i < from+nodesPerPage; i++ {
		n := &ns[i]
		label := fmt.Sprintf("%s %s · %s", nodeBadge(n), cut(n.Name, 18), cut(n.Address, 22))
		if n.UpdateTo != "" {
			label += " · ⬆️"
		}
		kb = append(kb, []Button{btn(label, "nv", n.ID)})
	}
	if p := pager("nl", page, pages); p != nil {
		kb = append(kb, p)
	}
	kb = append(kb, []Button{btn(T("➕ نود جدید"), "nn"), btn(T("🔄 تازه کردن"), "nl", page)}, nav("home"))
	r.show(view{text: text, kb: kb})
}

func nodeText(n *nodeRow, r *req) string {
	t := header("🌐", n.Name)
	t += T("وضعیت: ") + nodeStateText(n) + "\n"
	t += T("آدرس: ") + code(n.Address) + T(" · پورت نود: ") + code(strconv.Itoa(n.APIPort)) + "\n"
	if n.State.Version != "" {
		t += T("نسخه نود: ") + code(n.State.Version)
		if n.State.XrayVersion != "" {
			t += " · Xray: " + code(n.State.XrayVersion)
		}
		t += "\n"
	}
	if n.State.Status == "online" || n.State.Status == "degraded" {
		sync := T("✅ همگام")
		if !n.State.Synced {
			sync = T("⏳ در حال همگام سازی")
		}
		t += sync + " · ⏱ " + faInt(n.State.LatencyMS) + " ms"
		if n.State.CPUs > 0 {
			t += T(" · بار: ") + fa(strconv.FormatFloat(n.State.Load1, 'f', 2, 64)) + " / " + faInt(n.State.CPUs) + T(" هسته")
		}
		if n.State.MemUsed > 0 {
			t += T(" · رم: ") + fa(strconv.FormatFloat(n.State.MemUsed, 'f', 0, 64)) + T("٪")
		}
		t += "\n"
	}
	t += T("📥 ورودی ها: ") + faInt(len(n.InboundIDs)) + " · ⬆️ " + size(n.Up) + " · ⬇️ " + size(n.Down) + "\n"
	if n.State.Error != "" && n.State.Status != "online" {
		t += "⚠️ " + esc(cut(n.State.Error, 300)) + "\n"
	}
	if n.UpdateTo != "" {
		t += T("\n⬆️ نسخه تازه برای نود: ") + code(n.UpdateTo) + "\n"
		if !n.State.CanUpdate {
			t += T("این نود هنوز بروزرسانی با یک دکمه را ندارد: یک بار دستور نصب تازه را روی سرور نود اجرا کنید.\n")
		}
	}
	if len(n.State.NeedsUpdate) > 0 {
		var names []string
		for _, x := range n.State.NeedsUpdate {
			names = append(names, esc(x.Remark))
		}
		t += T("⚠️ این ورودی ها تا بروزرسانی نود اجرا نمی شوند: ") + strings.Join(names, T("، ")) + "\n"
	}
	if u := n.State.Update; u != nil && u.Phase != "" {
		t += T("\nآخرین بروزرسانی: ") + code(u.Target) + " · " + nodeUpdPhase[u.Phase]
		if u.Error != "" {
			t += "\n⚠️ " + esc(cut(u.Error, 300))
		}
		t += "\n"
	}
	return t
}

func nodeView(r *req, a []string) {
	id := argInt(a, 0)
	n, err := r.node(id)
	if err != nil {
		r.fail(err, "nl")
		return
	}
	kb := Keyboard{}
	if n.UpdateTo != "" && n.State.CanUpdate && n.Enabled {
		kb = append(kb, []Button{btn(T("⬆️ بروزرسانی نود به ")+n.UpdateTo, "nu", id)})
	}
	toggle := btn(T("⛔️ خاموش کردن"), "nt", id)
	if !n.Enabled {
		toggle = btn(T("✅ روشن کردن"), "nt", id)
	}
	kb = append(kb,
		[]Button{toggle, btn(T("🔄 تازه کردن"), "nv", id)},
		[]Button{btn(T("🔑 دستور نصب تازه"), "ni", id), btn(T("🗑 حذف"), "nd", id)},
		nav("nl"))
	r.show(view{text: nodeText(n, r), kb: kb})
}

func nodeToggle(r *req, a []string) {
	id := argInt(a, 0)
	n, err := r.node(id)
	if err != nil {
		r.fail(err, "nl")
		return
	}
	r.toast("⏳")
	if err := r.call("PATCH", fmt.Sprintf("/nodes/%d", id), map[string]any{"enabled": !n.Enabled}, nil); err != nil {
		r.fail(err, cbd("nv", id))
		return
	}
	nodeView(r, a)
}

func nodeUpdate(r *req, a []string) {
	id := argInt(a, 0)
	r.progress(T("در حال شروع بروزرسانی نود"))
	if err := r.call("POST", fmt.Sprintf("/nodes/%d/update", id), nil, nil); err != nil {
		r.fail(err, cbd("nv", id))
		return
	}
	nodeUpdateWatch(r, a)
}

// nodeUpdateWatch follows a node's agent update until it finishes.
func nodeUpdateWatch(r *req, a []string) {
	id := argInt(a, 0)
	start := time.Now()
	r.watch(3*time.Minute, func(w *req) (view, bool) {
		n, err := w.node(id)
		if err != nil {
			return view{text: header("⚠️", T("انجام نشد")) + esc(errText(err)), kb: Keyboard{nav("nl")}}, true
		}
		u := n.State.Update
		t := header("⬆️", T("بروزرسانی نود ")+n.Name)
		if u == nil {
			return view{text: t + T("اطلاعی از بروزرسانی نیست."), kb: Keyboard{nav(cbd("nv", id))}}, true
		}
		t += T("از ") + code(u.From) + T(" به ") + code(u.Target) + "\n\n"
		steps := []string{"downloading", "verifying", "restarting", "done"}
		cur := -1
		for i, s := range steps {
			if s == u.Phase {
				cur = i
			}
		}
		for i, s := range steps[:3] {
			mark := "▫️"
			switch {
			case u.Phase == "done" || i < cur:
				mark = "✅"
			case i == cur:
				mark = "⏳"
			}
			t += mark + " " + nodeUpdPhase[s] + "\n"
		}
		done := u.Phase == "done" || u.Phase == "failed"
		switch u.Phase {
		case "done":
			t += T("\n✅ نود با نسخه ") + code(u.Target) + T(" دوباره وصل شد.")
		case "failed":
			t += T("\n⚠️ بروزرسانی انجام نشد: ") + esc(cut(u.Error, 300))
			if u.RolledBack {
				t += T("\nنسخه قبلی نود برگردانده شد.")
			}
		default:
			t += "\n⏳ " + fa(strconv.Itoa(int(time.Since(start).Seconds()))) + T(" ثانیه · این صفحه خودکار تازه می شود.")
		}
		kb := Keyboard{}
		if !done {
			kb = append(kb, []Button{btn(T("🔄 تازه کردن"), "nw", id)})
		}
		kb = append(kb, nav(cbd("nv", id)))
		return view{text: t, kb: kb}, done
	})
}

func nodeAskPort(r *req, warn string) {
	r.ask("nnport", nil, T("نود جدید · ")+cut(r.st.draft["name"], 20),
		warn+T("پورت ارتباط پنل با نود را بفرستید یا پیش فرض را بزنید. این پورت باید روی سرور نود آزاد باشد:"), "nl",
		[]Button{btn(T("پیش فرض ۶۲۰۵۰"), "nnp", 62050)})
}

func nodeCreate(r *req) {
	port, _ := strconv.Atoi(r.st.draft["port"])
	body := map[string]any{"name": r.st.draft["name"], "address": r.st.draft["addr"], "api_port": port}
	r.progress(T("در حال ساختن نود"))
	var res struct {
		Node    nodeRow     `json:"node"`
		Install nodeInstall `json:"install"`
	}
	if err := r.call("POST", "/nodes", body, &res); err != nil {
		r.fail(err, "nl")
		return
	}
	r.st.draft = map[string]string{}
	nodeShowInstall(r, res.Node.ID, res.Node.Name, &res.Install)
}

// nodeShowInstall shows the one-time install command of a node.
func nodeShowInstall(r *req, id int64, name string, inst *nodeInstall) {
	t := header("🔑", T("دستور نصب نود ")+name)
	asFile := len(inst.Command) > 3000
	if asFile {
		// Too long for one message: sent as a file above the menu.
		err := r.cl.SendDocument(r.ctx, r.chat, fmt.Sprintf("veilon-node-%d-install.txt", id), []byte(inst.Command+"\n"), T("🔑 دستور نصب نود ")+esc(name))
		if err != nil {
			r.fail(err, cbd("nv", id))
			return
		}
		t += T("دستور نصب در فایل بالا فرستاده شد. متن آن را روی سرور نود (با دسترسی root) اجرا کنید.\n\n")
	} else {
		t += T("این دستور را روی سرور نود (با دسترسی root) اجرا کنید:\n\n<pre>") + esc(inst.Command) + "</pre>\n\n"
	}
	t += T("⚠️ این دستور فقط یک بار کار می کند و کلید اتصال نود در آن است؛ آن را به کسی ندهید.\n")
	if inst.Port > 0 {
		t += T("پورت ") + code(strconv.Itoa(inst.Port)) + T(" باید روی سرور نود برای این سرور باز باشد.\n")
	}
	v := view{text: t, kb: Keyboard{{btn(T("🌐 دیدن نود"), "nv", id)}, nav("nl")}}
	if asFile {
		r.fresh(v)
		return
	}
	r.show(v)
}
