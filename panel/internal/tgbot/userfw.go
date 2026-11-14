package tgbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
)

// Per-user firewall: the admin's own list of domains and IPs blocked for
// one user (GET/POST/DELETE /users/{id}/firewall).

const fwPerPage = 10

func init() {
	on("ufw", userFirewall)
	on("ufwa", func(r *req, a []string) {
		id := argInt(a, 0)
		r.ask("ufwadd", []string{strconv.FormatInt(id, 10)}, T("افزودن به فایروال"), fwPrompt(), cbd("ufw", id))
	})
	onInput("ufwadd", userFirewallAdd)
	on("ufwd", userFirewallDelete)
	on("ufwc", func(r *req, a []string) {
		id := argInt(a, 0)
		confirm(r, T("پاک کردن فایروال"), T("همه موارد فایروال این کاربر پاک شود؟ دسترسی کاربر به آن ها باز می شود."), cbd("ufwcy", id), cbd("ufw", id))
	})
	on("ufwcy", func(r *req, a []string) {
		id := argInt(a, 0)
		if err := r.call("DELETE", fmt.Sprintf("/users/%d/firewall", id), nil, nil); err != nil {
			r.fail(err, cbd("ufw", id))
			return
		}
		r.toast(T("🧹 فهرست پاک شد"))
		userFirewall(r, []string{strconv.FormatInt(id, 10)})
	})
}

type fwEntryView struct {
	Value     string `json:"value"`
	Kind      string `json:"kind"`
	AmneziaWG bool   `json:"amneziawg"`
}

type fwListView struct {
	Entries []fwEntryView `json:"entries"`
	Max     int           `json:"max"`
}

var fwKindLabel = map[string]string{
	"domain": T("دامنه و زیر دامنه ها"),
	"full":   T("فقط همین دامنه"),
	"regexp": T("الگوی دامنه"),
	"ip":     "IP",
}

func fwPrompt() string {
	return T("یک یا چند مورد بفرستید (هر کدام در یک خط، یا با فاصله و ویرگول جدا):\n") +
		"<code>example.com</code> " + T("دامنه و همه زیر دامنه ها") + "\n" +
		"<code>full:api.example.com</code> " + T("فقط همین دامنه") + "\n" +
		"<code>regexp:\\.example\\.net$</code> " + T("الگو") + "\n" +
		"<code>1.2.3.4</code> · <code>10.0.0.0/8</code> · <code>2001:db8::/32</code> IP / CIDR\n\n" +
		T("ℹ️ در AmneziaWG فقط IP و CIDR (IPv4) اعمال می شود؛ دامنه ها آنجا اثری ندارند.")
}

// fwHash names an entry in a callback (entries can be longer than a
// callback allows).
func fwHash(v string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(v))
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}

func userFirewall(r *req, a []string) {
	id := argInt(a, 0)
	page := int(argInt(a, 1))
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	var fw fwListView
	if err := r.call("GET", fmt.Sprintf("/users/%d/firewall", id), nil, &fw); err != nil {
		r.fail(err, cbd("ue", id))
		return
	}
	r.show(firewallView(id, u.Username, fw, page, ""))
}

func firewallView(id int64, name string, fw fwListView, page int, notice string) view {
	text := header("🛡", T("فایروال ")+name)
	if notice != "" {
		text += notice + "\n\n"
	}
	text += T("دامنه ها و IP های این فهرست فقط برای همین کاربر بسته می شوند، تا وقتی که پاکشان کنید.\n")
	pages := (len(fw.Entries) + fwPerPage - 1) / fwPerPage
	if page < 0 || page >= pages {
		page = 0
	}
	kb := Keyboard{}
	if len(fw.Entries) == 0 {
		text += T("\n<i>فهرست خالی است.</i>\n")
	} else {
		text += "\n" + faInt(len(fw.Entries)) + T(" مورد از حداکثر ") + faInt(fw.Max) + "\n"
		var dels []Button
		for i := page * fwPerPage; i < len(fw.Entries) && i < (page+1)*fwPerPage; i++ {
			e := fw.Entries[i]
			text += faInt(i+1) + ". " + code(e.Value) + " · " + fwKindLabel[e.Kind] + "\n"
			dels = append(dels, btn("🗑 "+faInt(i+1), "ufwd", id, fwHash(e.Value), page))
		}
		kb = append(kb, chunk(dels, 5)...)
		if pages > 1 {
			var row []Button
			if page > 0 {
				row = append(row, btn(T("◀️ قبلی"), "ufw", id, page-1))
			}
			row = append(row, btn(faInt(page+1)+" / "+faInt(pages), "ufw", id, page))
			if page < pages-1 {
				row = append(row, btn(T("بعدی ▶️"), "ufw", id, page+1))
			}
			kb = append(kb, row)
		}
	}
	text += T("\nℹ️ در AmneziaWG فقط IP و CIDR (IPv4) اعمال می شود؛ دامنه ها آنجا اثری ندارند.")
	top := []Button{btn(T("➕ افزودن"), "ufwa", id)}
	if len(fw.Entries) > 0 {
		top = append(top, btn(T("🧹 پاک کردن همه"), "ufwc", id))
	}
	kb = append(Keyboard{top}, kb...)
	kb = append(kb, nav(cbd("ue", id)))
	return view{text: text, kb: kb}
}

// splitEntries splits typed text into entries (lines, spaces, commas,
// Persian commas).
func splitEntries(s string) []string {
	return strings.FieldsFunc(s, func(c rune) bool {
		return c == '\n' || c == '\r' || c == ' ' || c == '\t' || c == ',' || c == '،' || c == ';'
	})
}

func userFirewallAdd(r *req, a []string, text string, _ *Document) {
	id := argInt(a, 0)
	entries := splitEntries(text)
	again := func(msg string) {
		r.ask("ufwadd", a, T("افزودن به فایروال"), "⚠️ "+msg+"\n\n"+fwPrompt(), cbd("ufw", id))
	}
	if len(entries) == 0 {
		again(T("چیزی فرستاده نشد."))
		return
	}
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	before := len(u.Firewall)
	var fw fwListView
	if err := r.call("POST", fmt.Sprintf("/users/%d/firewall", id), map[string]any{"entries": entries}, &fw); err != nil {
		if msg := fwErrText(err); msg != "" {
			again(msg)
			return
		}
		r.fail(err, cbd("ufw", id))
		return
	}
	added := len(fw.Entries) - before
	notice := "✅ " + faInt(added) + T(" مورد اضافه شد.")
	if added < len(entries) {
		notice += T(" (موارد تکراری کنار گذاشته شد)")
	}
	r.show(firewallView(id, u.Username, fw, (len(fw.Entries)-1)/fwPerPage, notice))
}

// fwErrText explains a refused entry in the admin's language ("" when the
// error is not about an entry).
func fwErrText(err error) string {
	var ae *apiErr
	if !errors.As(err, &ae) || ae.Status != 400 {
		return ""
	}
	var e struct {
		Error struct {
			Field  string `json:"field"`
			Entry  string `json:"entry"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(ae.Raw, &e) != nil || e.Error.Field != "firewall" {
		return ""
	}
	why := map[string]string{
		"too_long":    T("بیش از حد طولانی است"),
		"bad_domain":  T("دامنه درست نیست"),
		"bad_regexp":  T("الگو (regexp) درست نیست"),
		"bad_ip":      T("IP یا CIDR درست نیست"),
		"too_broad":   T("بازه بیش از حد بزرگ است (IPv4 حداقل /8 و IPv6 حداقل /16)"),
		"unsupported": T("این پیشوند پشتیبانی نمی شود (فقط دامنه، full:، regexp:، IP یا CIDR)"),
		"empty":       T("خالی است"),
	}[e.Error.Reason]
	if e.Error.Reason == "too_many" {
		return T("حداکثر ۲۰۰ مورد برای هر کاربر مجاز است.")
	}
	if why == "" {
		return ""
	}
	return code(e.Error.Entry) + ": " + why
}

func userFirewallDelete(r *req, a []string) {
	id, h, page := argInt(a, 0), argStr(a, 1), int(argInt(a, 2))
	var fw fwListView
	if err := r.call("GET", fmt.Sprintf("/users/%d/firewall", id), nil, &fw); err != nil {
		r.fail(err, cbd("ue", id))
		return
	}
	val := ""
	for _, e := range fw.Entries {
		if fwHash(e.Value) == h {
			val = e.Value
			break
		}
	}
	if val == "" {
		r.toast(T("این مورد دیگر در فهرست نیست"))
		userFirewall(r, []string{strconv.FormatInt(id, 10), strconv.Itoa(page)})
		return
	}
	if err := r.call("DELETE", fmt.Sprintf("/users/%d/firewall?entry=%s", id, url.QueryEscape(val)), nil, nil); err != nil {
		r.fail(err, cbd("ufw", id))
		return
	}
	r.toast(T("🗑 حذف شد"))
	userFirewall(r, []string{strconv.FormatInt(id, 10), strconv.Itoa(page)})
}
