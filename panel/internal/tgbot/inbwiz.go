package tgbot

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// The inbound wizard: a new inbound of any protocol, or the security and
// transport of an existing one, one question per screen with a back button
// and a review screen before anything is saved. Every choice the web wizard
// has is asked; the allowed transports and securities come from the shared
// table in package inbounds (inbounds.NetworksFor, SecuritiesFor,
// FlowAllowed), the same one the web wizard and the server use. A typical
// value is only ever offered as a button, never filled in silently. The API
// validates the result.

// wizState is the wizard draft, kept as JSON in the chat draft.
type wizState struct {
	// Edit is the inbound whose security and transport are edited (0: new).
	Edit int64         `json:"edit,omitempty"`
	In   inbounds.View `json:"in"`
	// Done lists the answered questions in the order they were answered.
	Done []string `json:"done"`
	// Cur is the question on screen ("" = the review).
	Cur      string `json:"cur"`
	CertName string `json:"cert_name,omitempty"`
	// Self: the admin asked for a new self-signed certificate (its name
	// is the next question).
	Self  bool      `json:"self,omitempty"`
	Nodes []wizNode `json:"nodes,omitempty"`
	// warn is a warning a button left for the screen (not kept).
	warn string
}

type wizNode struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (w *wizState) done(k string) bool {
	for _, x := range w.Done {
		if x == k {
			return true
		}
	}
	return false
}

func (w *wizState) mark(k string) {
	w.unmark(k)
	w.Done = append(w.Done, k)
}

func (w *wizState) unmark(k string) {
	out := w.Done[:0]
	for _, x := range w.Done {
		if x != k {
			out = append(out, x)
		}
	}
	w.Done = out
}

func wizLoad(r *req) *wizState {
	raw := r.st.draft["wiz"]
	if raw == "" {
		return nil
	}
	var w wizState
	if json.Unmarshal([]byte(raw), &w) != nil {
		return nil
	}
	return &w
}

func wizSave(r *req, w *wizState) {
	b, _ := json.Marshal(w)
	if r.st.draft == nil {
		r.st.draft = map[string]string{}
	}
	r.st.draft["wiz"] = string(b)
}

// wopt is one button of a question.
type wopt struct {
	label string
	on    bool
	set   func(r *req, w *wizState)
}

// wfield is one question.
type wfield struct {
	key   string
	title string
	hint  func(w *wizState) string
	when  func(w *wizState) bool
	show  func(w *wizState) string
	opts  func(r *req, w *wizState) []wopt
	// multi: the buttons switch values on and off; "confirm" moves on.
	multi bool
	// input reads a typed value ("" = accepted, else a warning).
	input func(r *req, w *wizState, text string) string
	// ok reports whether the answer is still valid (an answer a later
	// change made invalid is asked again).
	ok func(w *wizState) bool
	// edit: part of editing the security and transport of an inbound.
	edit bool
}

var wizFields []wfield

func wizField(k string) *wfield {
	for i := range wizFields {
		if wizFields[i].key == k {
			return &wizFields[i]
		}
	}
	return nil
}

func (w *wizState) applies(f *wfield) bool {
	if w.Edit != 0 && !f.edit {
		return false
	}
	return f.when == nil || f.when(w)
}

// --- small helpers -------------------------------------------------------

func (w *wizState) p() string { return w.In.Protocol }
func (w *wizState) v2() bool  { return inbounds.IsV2(w.In.Protocol) }
func (w *wizState) isTLS() bool {
	return w.In.Security == "tls"
}
func (w *wizState) isReality() bool { return w.In.Security == "reality" }
func (w *wizState) tls() *inbounds.TLS {
	if w.In.TLS == nil {
		w.In.TLS = &inbounds.TLS{}
	}
	return w.In.TLS
}
func (w *wizState) reality() *inbounds.Reality {
	if w.In.Reality == nil {
		w.In.Reality = &inbounds.Reality{}
	}
	return w.In.Reality
}
func (w *wizState) enc() *inbounds.VLESSEnc {
	if w.In.VLESSEnc == nil {
		w.In.VLESSEnc = &inbounds.VLESSEnc{}
	}
	return w.In.VLESSEnc
}
func (w *wizState) encOn() bool { return w.In.VLESSEnc != nil && w.In.VLESSEnc.Enabled }
func (w *wizState) pathNet() bool {
	n := w.In.Network
	return w.v2() && (n == "ws" || n == "httpupgrade" || n == "xhttp")
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func onText(b bool) string {
	if b {
		return T("روشن")
	}
	return T("خاموش")
}

func joinList(l []string) string { return strings.Join(l, ", ") }

func wizList(s string) []string {
	out := []string{}
	for _, x := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '،' || r == ' ' || r == '\n' }) {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// choice builds the buttons of a pick-one question.
func choice(cur string, isDone bool, vals []string, label func(string) string, set func(w *wizState, v string)) []wopt {
	var out []wopt
	for _, v := range vals {
		v := v
		out = append(out, wopt{label: label(v), on: isDone && cur == v, set: func(_ *req, w *wizState) { set(w, v) }})
	}
	return out
}

func same(s string) string { return s }

// sugg is a suggestion button for a typed value.
func sugg(label string, set func(w *wizState)) wopt {
	return wopt{label: label, set: func(_ *req, w *wizState) { set(w) }}
}

func boolOpts(cur, isDone bool, set func(w *wizState, v bool)) []wopt {
	return []wopt{
		{label: T("✅ روشن"), on: isDone && cur, set: func(_ *req, w *wizState) { set(w, true) }},
		{label: T("▫️ خاموش"), on: isDone && !cur, set: func(_ *req, w *wizState) { set(w, false) }},
	}
}

func numIn(text string, min, max int64, set func(n int)) string {
	n, ok := parseInt(text)
	if !ok || n < min || n > max {
		return Tf("⚠️ عددی از %s تا %s بفرستید.", fa(strconv.FormatInt(min, 10)), fa(strconv.FormatInt(max, 10)))
	}
	set(int(n))
	return ""
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// portHint is a typical port for the protocol (offered as a button only).
func portHint(p string) int {
	switch p {
	case inbounds.VLESS, inbounds.Trojan, inbounds.Hysteria, inbounds.TUIC, inbounds.Masque, inbounds.ShadowTLS, inbounds.AnyTLS, inbounds.Naive:
		return 443
	case inbounds.WireGuard:
		return 51820
	case inbounds.AmneziaWG:
		return 51821
	case inbounds.HTTP:
		return 8080
	case inbounds.Mixed, inbounds.Socks:
		return 1080
	}
	return 2096
}

var netLabels = map[string]string{"tcp": "tcp · RAW", "ws": "ws · WebSocket", "grpc": "grpc · gRPC", "httpupgrade": "httpupgrade · HTTPUpgrade", "xhttp": "xhttp · XHTTP", "kcp": "kcp · mKCP"}

func secLabel(p, s string) string {
	if p == inbounds.HTTP {
		if s == "tls" {
			return T("🔒 HTTPS (با TLS)")
		}
		return T("🌐 HTTP (بدون TLS)")
	}
	switch s {
	case "tls":
		return "🔒 TLS"
	case "reality":
		return "✨ Reality"
	}
	return T("🚫 بدون امنیت")
}

type certRow struct {
	ID     int64  `json:"id"`
	Domain string `json:"domain"`
	Source string `json:"source"`
}

func (r *req) certs() []certRow {
	var list []certRow
	_ = r.call("GET", "/certs", nil, &list)
	return list
}

// --- the questions -------------------------------------------------------

func init() {
	wizFields = []wfield{
		{key: "remark", title: T("نام ورودی"),
			hint: func(w *wizState) string { return T("یک نام برای ورودی بفرستید.") },
			show: func(w *wizState) string { return w.In.Remark },
			opts: func(r *req, w *wizState) []wopt {
				var list []inbound
				_ = r.call("GET", "/inbounds", nil, &list)
				used := map[string]bool{}
				for _, ib := range list {
					used[strings.ToLower(ib.Remark)] = true
				}
				name := protoLabel(w.p())
				for i := 2; used[strings.ToLower(name)]; i++ {
					name = fmt.Sprintf("%s %d", protoLabel(w.p()), i)
				}
				return []wopt{sugg(T("پیشنهاد: ")+name, func(w *wizState) { w.In.Remark = name })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if text == "" || len(text) > 128 {
					return T("⚠️ نام باید ۱ تا ۱۲۸ حرف باشد.")
				}
				w.In.Remark = text
				return ""
			}},
		{key: "port", title: T("پورت"),
			when: func(w *wizState) bool { return inbounds.HasPort(w.p()) },
			hint: func(w *wizState) string { return T("پورت را بفرستید (۱ تا ۶۵۵۳۵).") },
			show: func(w *wizState) string { return strconv.Itoa(w.In.Port) },
			opts: func(_ *req, w *wizState) []wopt {
				rp, hp := 10000+mrand.IntN(50000), portHint(w.p())
				return []wopt{
					sugg(T("🎲 پورت تصادفی ")+fa(strconv.Itoa(rp)), func(w *wizState) { w.In.Port = rp }),
					sugg(T("پیشنهاد: ")+fa(strconv.Itoa(hp)), func(w *wizState) { w.In.Port = hp }),
				}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 1, 65535, func(n int) { w.In.Port = n })
			}},
		{key: "listen", title: T("آدرس گوش دادن"),
			when: func(w *wizState) bool { return inbounds.HasPort(w.p()) },
			hint: func(w *wizState) string {
				return T("IP ای از سرور که ورودی روی آن گوش بدهد را بفرستید، یا همه آدرس ها را انتخاب کنید.")
			},
			show: func(w *wizState) string {
				if w.In.Listen == "" {
					return T("همه آدرس ها")
				}
				return w.In.Listen
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("همه آدرس ها (0.0.0.0)"), func(w *wizState) { w.In.Listen = "" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if net.ParseIP(text) == nil {
					return T("⚠️ یک IP درست بفرستید.")
				}
				w.In.Listen = text
				return ""
			}},
		// Transport.
		{key: "net", title: T("نوع انتقال (transport)"), edit: true,
			when: func(w *wizState) bool { return len(inbounds.NetworksFor(w.p())) > 0 },
			hint: func(w *wizState) string {
				return T("ترافیک با کدام روش منتقل شود؟ Reality فقط روی tcp، grpc و xhttp کار می کند.")
			},
			show: func(w *wizState) string { return w.In.Network },
			ok:   func(w *wizState) bool { return containsS(inbounds.NetworksFor(w.p()), w.In.Network) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Network, w.done("net"), inbounds.NetworksFor(w.p()), func(s string) string { return netLabels[s] }, func(w *wizState, n string) {
					if n != w.In.Network {
						w.In.Transport = inbounds.Transport{}
						for _, k := range []string{"path", "host", "svc", "gmode", "xmode"} {
							w.unmark(k)
						}
					}
					w.In.Network = n
				})
			}},
		{key: "path", title: T("مسیر (path)"), edit: true,
			when: func(w *wizState) bool { return w.pathNet() },
			hint: func(w *wizState) string { return T("مسیر را بفرستید (با / شروع می شود).") },
			show: func(w *wizState) string { return w.In.Transport.Path },
			opts: func(_ *req, w *wizState) []wopt {
				d := "/" + w.In.Network
				return []wopt{sugg(T("پیش فرض: ")+d, func(w *wizState) { w.In.Transport.Path = d })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " \t") {
					return T("⚠️ مسیر نباید فاصله داشته باشد.")
				}
				if !strings.HasPrefix(text, "/") {
					text = "/" + text
				}
				w.In.Transport.Path = text
				return ""
			}},
		{key: "host", title: T("هاست (host)"), edit: true,
			when: func(w *wizState) bool { return w.pathNet() },
			hint: func(w *wizState) string {
				return T("دامنه هدر Host را بفرستید، یا خالی بگذارید.")
			},
			show: func(w *wizState) string { return dash(w.In.Transport.Host) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("خالی"), func(w *wizState) { w.In.Transport.Host = "" })}
			},
			input: func(_ *req, w *wizState, text string) string { w.In.Transport.Host = text; return "" }},
		{key: "xmode", title: T("حالت XHTTP"), edit: true,
			when: func(w *wizState) bool { return w.v2() && w.In.Network == "xhttp" },
			show: func(w *wizState) string { return w.In.Transport.Mode },
			ok:   func(w *wizState) bool { return containsS(inbounds.XHTTPModes, w.In.Transport.Mode) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Transport.Mode, w.done("xmode"), inbounds.XHTTPModes, same, func(w *wizState, s string) { w.In.Transport.Mode = s })
			}},
		{key: "svc", title: T("نام سرویس gRPC"), edit: true,
			when: func(w *wizState) bool { return w.v2() && w.In.Network == "grpc" },
			hint: func(w *wizState) string { return T("نام سرویس gRPC را بفرستید.") },
			show: func(w *wizState) string { return w.In.Transport.ServiceName },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: grpc"), func(w *wizState) { w.In.Transport.ServiceName = "grpc" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " \t/") {
					return T("⚠️ نام سرویس نباید فاصله یا / داشته باشد.")
				}
				w.In.Transport.ServiceName = text
				return ""
			}},
		{key: "gmode", title: T("حالت gRPC"), edit: true,
			when: func(w *wizState) bool { return w.v2() && w.In.Network == "grpc" },
			show: func(w *wizState) string { return w.In.Transport.Mode },
			ok:   func(w *wizState) bool { return containsS(inbounds.GRPCModes, w.In.Transport.Mode) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Transport.Mode, w.done("gmode"), inbounds.GRPCModes, same, func(w *wizState, s string) { w.In.Transport.Mode = s })
			}},
		{key: "ssm", title: T("روش رمزگذاری Shadowsocks"),
			when: func(w *wizState) bool { return w.p() == inbounds.Shadowsocks },
			show: func(w *wizState) string { return w.In.SSMethod },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.SSMethod, w.done("ssm"), inbounds.SSMethods, same, func(w *wizState, s string) { w.In.SSMethod = s })
			}},
		// Protocol settings.
		{key: "obfs", title: T("رمز obfs (Salamander)"),
			when: func(w *wizState) bool { return w.p() == inbounds.Hysteria },
			hint: func(w *wizState) string {
				return T("با رمز obfs، بسته های QUIC شبیه داده تصادفی می شوند. رمز را بفرستید یا بدون obfs را بزنید.")
			},
			show: func(w *wizState) string { return dash(w.In.Settings.ObfsPassword) },
			opts: func(*req, *wizState) []wopt {
				pw := randHex(8)
				return []wopt{sugg(T("بدون obfs"), func(w *wizState) { w.In.Settings.ObfsPassword = "" }),
					sugg(T("🎲 رمز تصادفی"), func(w *wizState) { w.In.Settings.ObfsPassword = pw })}
			},
			input: func(_ *req, w *wizState, text string) string { w.In.Settings.ObfsPassword = text; return "" }},
		{key: "up", title: T("سرعت آپلود سرور (Mbps)"),
			when: func(w *wizState) bool { return w.p() == inbounds.Hysteria },
			hint: func(w *wizState) string {
				return T("۰ یعنی بدون سقف (کلاینت ها سرعت را خودشان تنظیم می کنند).")
			},
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.UpMbps)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("۰ (بدون سقف)"), func(w *wizState) { w.In.Settings.UpMbps = 0 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 0, 100000, func(n int) { w.In.Settings.UpMbps = n })
			}},
		{key: "down", title: T("سرعت دانلود سرور (Mbps)"),
			when: func(w *wizState) bool { return w.p() == inbounds.Hysteria },
			hint: func(w *wizState) string {
				return T("۰ یعنی بدون سقف (کلاینت ها سرعت را خودشان تنظیم می کنند).")
			},
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.DownMbps)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("۰ (بدون سقف)"), func(w *wizState) { w.In.Settings.DownMbps = 0 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 0, 100000, func(n int) { w.In.Settings.DownMbps = n })
			}},
		{key: "hop", title: T("پرش پورت"),
			when: func(w *wizState) bool { return w.p() == inbounds.Hysteria },
			hint: func(w *wizState) string {
				return T("بازه پورت های UDP را به شکل شروع-پایان بفرستید و چند بازه را با ویرگول جدا کنید. نمونه:\n20000-30000")
			},
			show: func(w *wizState) string {
				if w.In.Settings.HopPorts == "" {
					return T("خاموش")
				}
				return w.In.Settings.HopPorts
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("خاموش"), func(w *wizState) { w.In.Settings.HopPorts = "" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				rs, err := inbounds.ParseHopPorts(text)
				if err != nil {
					return "⚠️ " + esc(err.Error())
				}
				w.In.Settings.HopPorts = inbounds.FormatHopPorts(rs)
				return ""
			}},
		{key: "hopi", title: T("فاصله پرش (ثانیه)"),
			when: func(w *wizState) bool { return w.p() == inbounds.Hysteria && w.In.Settings.HopPorts != "" },
			hint: func(w *wizState) string {
				return T("کلاینت ها هر چند ثانیه پورت عوض کنند؟ ۵ تا ۳۶۰۰، یا پیش فرض کلاینت.")
			},
			show: func(w *wizState) string {
				if w.In.Settings.HopInterval == 0 {
					return T("پیش فرض کلاینت")
				}
				return fa(strconv.Itoa(w.In.Settings.HopInterval))
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض کلاینت"), func(w *wizState) { w.In.Settings.HopInterval = 0 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 5, 3600, func(n int) { w.In.Settings.HopInterval = n })
			}},
		{key: "cong", title: T("کنترل ازدحام TUIC"),
			when: func(w *wizState) bool { return w.p() == inbounds.TUIC },
			show: func(w *wizState) string { return w.In.Settings.Congestion },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Settings.Congestion, w.done("cong"), inbounds.TUICCongestions, same, func(w *wizState, s string) { w.In.Settings.Congestion = s })
			}},
		{key: "pool", title: T("بازه IP کلاینت ها (MASQUE)"),
			when: func(w *wizState) bool { return w.p() == inbounds.Masque },
			hint: func(w *wizState) string {
				return T("یک بازه IPv4 و/یا یک بازه IPv6 (آدرس سرور با پیشوند) را با ویرگول جدا بفرستید.")
			},
			show: func(w *wizState) string { return joinList(w.In.Settings.Pool) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"10.14.0.1/24, fd14::1/64", func(w *wizState) { w.In.Settings.Pool = []string{"10.14.0.1/24", "fd14::1/64"} })}
			},
			input: func(_ *req, w *wizState, text string) string {
				l := wizList(text)
				if len(l) == 0 {
					return T("⚠️ حداقل یک بازه بفرستید.")
				}
				w.In.Settings.Pool = l
				return ""
			}},
		{key: "mmtu", title: "MTU",
			when: func(w *wizState) bool { return w.p() == inbounds.Masque },
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.MTU)) },
			hint: func(w *wizState) string { return T("عددی از ۱۲۸۰ تا ۶۵۵۳۵ بفرستید.") },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+fa("1280"), func(w *wizState) { w.In.Settings.MTU = 1280 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 1280, 65535, func(n int) { w.In.Settings.MTU = n })
			}},
		{key: "mpath", title: T("مسیر MASQUE"),
			when: func(w *wizState) bool { return w.p() == inbounds.Masque },
			show: func(w *wizState) string { return w.In.Settings.Path },
			hint: func(w *wizState) string { return T("مسیر را بفرستید (با / شروع می شود).") },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض استاندارد"), func(w *wizState) { w.In.Settings.Path = inbounds.MasqueDefaultPath })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if !strings.HasPrefix(text, "/") {
					return T("⚠️ مسیر باید با / شروع شود.")
				}
				w.In.Settings.Path = text
				return ""
			}},
		{key: "hs", title: T("سرور دست دهی"),
			when: func(w *wizState) bool { return w.p() == inbounds.ShadowTLS },
			hint: func(w *wizState) string {
				return T("دامنه یک سایت واقعی با TLS 1.3 که ناظر ها می بینند را بفرستید.")
			},
			show: func(w *wizState) string { return w.In.Settings.HandshakeServer },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"www.microsoft.com", func(w *wizState) { w.In.Settings.HandshakeServer = "www.microsoft.com" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " /:") {
					return T("⚠️ فقط نام دامنه را بفرستید (پورت در مرحله بعد).")
				}
				w.In.Settings.HandshakeServer = strings.ToLower(text)
				return ""
			}},
		{key: "hsp", title: T("پورت سرور دست دهی"),
			when: func(w *wizState) bool { return w.p() == inbounds.ShadowTLS },
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.HandshakePort)) },
			hint: func(w *wizState) string { return T("پورت را بفرستید (۱ تا ۶۵۵۳۵).") },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+fa("443"), func(w *wizState) { w.In.Settings.HandshakePort = 443 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 1, 65535, func(n int) { w.In.Settings.HandshakePort = n })
			}},
		{key: "stm", title: T("رمزگذاری Shadowsocks داخلی"),
			when: func(w *wizState) bool { return w.p() == inbounds.ShadowTLS },
			show: func(w *wizState) string { return w.In.SSMethod },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.SSMethod, w.done("stm"), inbounds.ShadowTLSMethods, same, func(w *wizState, s string) { w.In.SSMethod = s })
			}},
		{key: "pad", title: T("طرح padding (AnyTLS)"),
			when: func(w *wizState) bool { return w.p() == inbounds.AnyTLS },
			hint: func(w *wizState) string {
				return T("هر قانون در یک خط (خط اول stop=N). نمونه:\nstop=8\n0=30-30\n1=100-400")
			},
			show: func(w *wizState) string {
				if len(w.In.Settings.PaddingScheme) == 0 {
					return T("پیش فرض sing-box")
				}
				return strings.Join(w.In.Settings.PaddingScheme, " ; ")
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض sing-box"), func(w *wizState) { w.In.Settings.PaddingScheme = []string{} })}
			},
			input: func(_ *req, w *wizState, text string) string {
				var l []string
				for _, x := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ';' }) {
					if x = strings.TrimSpace(x); x != "" {
						l = append(l, x)
					}
				}
				if len(l) == 0 || !strings.HasPrefix(l[0], "stop=") {
					return T("⚠️ خط اول باید stop=N باشد.")
				}
				w.In.Settings.PaddingScheme = l
				return ""
			}},
		{key: "wgaddr", title: T("آدرس سرور در شبکه (WireGuard)"),
			when: func(w *wizState) bool { return inbounds.IsWG(w.p()) },
			hint: func(w *wizState) string {
				return T("آدرس IPv4 سرور با پیشوند (۱۶ تا ۳۰) را بفرستید؛ کاربران از همین بازه آدرس می گیرند.")
			},
			show: func(w *wizState) string { return dash(w.In.Settings.Address) },
			opts: func(*req, *wizState) []wopt {
				a := fmt.Sprintf("10.%d.%d.1/24", 64+mrand.IntN(63), mrand.IntN(256))
				return []wopt{sugg(T("پیشنهاد: ")+a, func(w *wizState) { w.In.Settings.Address = a })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if _, _, err := net.ParseCIDR(text); err != nil {
					return T("⚠️ نمونه درست: 10.66.66.1/24")
				}
				w.In.Settings.Address = text
				return ""
			}},
		{key: "dns", title: "DNS",
			when: func(w *wizState) bool { return inbounds.IsWG(w.p()) },
			hint: func(w *wizState) string {
				return T("IP های DNS کلاینت ها را با ویرگول جدا بفرستید.")
			},
			show: func(w *wizState) string { return dash(joinList(w.In.Settings.DNS)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"1.1.1.1, 8.8.8.8", func(w *wizState) { w.In.Settings.DNS = []string{"1.1.1.1", "8.8.8.8"} }),
					sugg(T("بدون DNS"), func(w *wizState) { w.In.Settings.DNS = []string{} })}
			},
			input: func(_ *req, w *wizState, text string) string {
				l := wizList(text)
				for _, x := range l {
					if net.ParseIP(x) == nil {
						return T("⚠️ فقط IP بفرستید.")
					}
				}
				w.In.Settings.DNS = l
				return ""
			}},
		{key: "wgmtu", title: "MTU",
			when: func(w *wizState) bool { return inbounds.IsWG(w.p()) },
			hint: func(w *wizState) string { return T("عددی از ۱۲۸۰ تا ۱۵۰۰ بفرستید.") },
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.MTU)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+fa("1420"), func(w *wizState) { w.In.Settings.MTU = 1420 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 1280, 1500, func(n int) { w.In.Settings.MTU = n })
			}},
		{key: "ka", title: T("Keepalive (ثانیه)"),
			when: func(w *wizState) bool { return inbounds.IsWG(w.p()) },
			hint: func(w *wizState) string {
				return T("عددی از ۰ تا ۶۰۰ بفرستید (۰ یعنی خاموش).")
			},
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.Keepalive)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+fa("25"), func(w *wizState) { w.In.Settings.Keepalive = 25 }),
					sugg(T("خاموش (۰)"), func(w *wizState) { w.In.Settings.Keepalive = 0 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 0, 600, func(n int) { w.In.Settings.Keepalive = n })
			}},
		{key: "wgkey", title: T("کلید خصوصی سرور"),
			when: func(w *wizState) bool { return inbounds.IsWG(w.p()) },
			hint: func(w *wizState) string {
				return T("کلید خصوصی WireGuard (base64) را بفرستید، یا بگذارید پنل بسازد.")
			},
			show: func(w *wizState) string {
				if w.In.Settings.PrivateKey == "" {
					return T("خودکار")
				}
				return T("کلید فرستاده شده")
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("خودکار (پنل می سازد)"), func(w *wizState) { w.In.Settings.PrivateKey = "" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if _, err := inbounds.WGPublicKey(text); err != nil {
					return T("⚠️ کلید درست نیست.")
				}
				w.In.Settings.PrivateKey = text
				return ""
			}},
		{key: "awg", title: T("پارامتر های AmneziaWG"),
			when: func(w *wizState) bool { return w.p() == inbounds.AmneziaWG },
			hint: func(w *wizState) string {
				return T("۹ عدد را به این ترتیب با فاصله بفرستید:\nJc Jmin Jmax S1 S2 H1 H2 H3 H4\nیا بگذارید پنل مقدار های تصادفی بسازد.")
			},
			show: func(w *wizState) string {
				a := w.In.Settings.AWG
				if a == nil {
					return T("خودکار")
				}
				return fmt.Sprintf("%d %d %d %d %d %d %d %d %d", a.Jc, a.Jmin, a.Jmax, a.S1, a.S2, a.H1, a.H2, a.H3, a.H4)
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("خودکار (پنل می سازد)"), func(w *wizState) { w.In.Settings.AWG = nil })}
			},
			input: func(_ *req, w *wizState, text string) string {
				f := wizList(text)
				var n [9]int64
				if len(f) != 9 {
					return T("⚠️ دقیقا ۹ عدد بفرستید.")
				}
				for i, x := range f {
					v, ok := parseInt(x)
					if !ok || v < 0 || v > 1<<32-1 {
						return T("⚠️ دقیقا ۹ عدد بفرستید.")
					}
					n[i] = v
				}
				w.In.Settings.AWG = &inbounds.AWG{Jc: int(n[0]), Jmin: int(n[1]), Jmax: int(n[2]), S1: int(n[3]), S2: int(n[4]),
					H1: uint32(n[5]), H2: uint32(n[6]), H3: uint32(n[7]), H4: uint32(n[8])}
				return ""
			}},
		{key: "tunname", title: T("نام رابط TUN"),
			when: func(w *wizState) bool { return w.p() == inbounds.TUN },
			hint: func(w *wizState) string { return T("۱ تا ۱۵ حرف، رقم، - یا _ بفرستید.") },
			show: func(w *wizState) string { return w.In.Settings.TunName },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"veilon-tun", func(w *wizState) { w.In.Settings.TunName = "veilon-tun" })}
			},
			input: func(_ *req, w *wizState, text string) string { w.In.Settings.TunName = text; return "" }},
		{key: "tunmtu", title: "MTU",
			when: func(w *wizState) bool { return w.p() == inbounds.TUN },
			hint: func(w *wizState) string { return T("عددی از ۵۷۶ تا ۹۰۰۰ بفرستید.") },
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.MTU)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+fa("1500"), func(w *wizState) { w.In.Settings.MTU = 1500 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 576, 9000, func(n int) { w.In.Settings.MTU = n })
			}},
		{key: "gw", title: T("آدرس رابط TUN"),
			when: func(w *wizState) bool { return w.p() == inbounds.TUN },
			hint: func(w *wizState) string {
				return T("یک آدرس IPv4 و/یا IPv6 با پیشوند، با ویرگول جدا.")
			},
			show: func(w *wizState) string { return joinList(w.In.Settings.Gateway) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"10.77.0.1/24", func(w *wizState) { w.In.Settings.Gateway = []string{"10.77.0.1/24"} })}
			},
			input: func(_ *req, w *wizState, text string) string {
				l := wizList(text)
				if len(l) == 0 {
					return T("⚠️ حداقل یک آدرس بفرستید.")
				}
				w.In.Settings.Gateway = l
				return ""
			}},
		{key: "routes", title: T("مسیر های TUN"),
			when: func(w *wizState) bool { return w.p() == inbounds.TUN },
			hint: func(w *wizState) string {
				return T("زیرشبکه هایی که از این رابط بروند را با ویرگول جدا بفرستید.")
			},
			show: func(w *wizState) string { return dash(joinList(w.In.Settings.Routes)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("هیچ"), func(w *wizState) { w.In.Settings.Routes = []string{} })}
			},
			input: func(_ *req, w *wizState, text string) string { w.In.Settings.Routes = wizList(text); return "" }},
		{key: "udp", title: "UDP",
			when: func(w *wizState) bool { return w.p() == inbounds.Mixed || w.p() == inbounds.Socks },
			hint: func(w *wizState) string {
				return T("UDP (مثلا برای DNS و بازی) هم پذیرفته شود؟")
			},
			show: func(w *wizState) string { return onText(w.In.Settings.UDP) },
			opts: func(_ *req, w *wizState) []wopt {
				return boolOpts(w.In.Settings.UDP, w.done("udp"), func(w *wizState, b bool) { w.In.Settings.UDP = b })
			}},
		{key: "auth", title: T("ورود با نام کاربری و رمز"),
			when: func(w *wizState) bool { return w.p() == inbounds.Socks },
			hint: func(w *wizState) string {
				return T("بدون ورود، هر کسی پورت را بداند وصل می شود و ترافیک به کاربران نسبت داده نمی شود.")
			},
			show: func(w *wizState) string { return onText(!w.In.Settings.NoAuth) },
			opts: func(_ *req, w *wizState) []wopt {
				return boolOpts(!w.In.Settings.NoAuth, w.done("auth"), func(w *wizState, b bool) { w.In.Settings.NoAuth = !b })
			}},
		{key: "taddr", title: T("مقصد تونل"),
			when: func(w *wizState) bool { return w.p() == inbounds.Tunnel },
			hint: func(w *wizState) string {
				return T("IP یا دامنه مقصد را بفرستید (مثلا 1.1.1.1).")
			},
			show: func(w *wizState) string { return w.In.Settings.TargetAddress },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg("127.0.0.1", func(w *wizState) { w.In.Settings.TargetAddress = "127.0.0.1" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " /") {
					return T("⚠️ فقط IP یا دامنه بفرستید.")
				}
				if h, ps, ok := strings.Cut(text, ":"); ok && net.ParseIP(text) == nil {
					if n, ok := parseInt(ps); ok && n > 0 && n < 65536 {
						w.In.Settings.TargetPort = int(n)
						w.mark("tport")
					}
					text = h
				}
				w.In.Settings.TargetAddress = text
				return ""
			}},
		{key: "tport", title: T("پورت مقصد تونل"),
			when: func(w *wizState) bool { return w.p() == inbounds.Tunnel },
			hint: func(w *wizState) string { return T("پورت را بفرستید (۱ تا ۶۵۵۳۵).") },
			show: func(w *wizState) string { return fa(strconv.Itoa(w.In.Settings.TargetPort)) },
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 1, 65535, func(n int) { w.In.Settings.TargetPort = n })
			}},
		{key: "tnet", title: T("شبکه تونل"),
			when: func(w *wizState) bool { return w.p() == inbounds.Tunnel },
			show: func(w *wizState) string { return w.In.Settings.TunnelNetwork },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Settings.TunnelNetwork, w.done("tnet"), inbounds.TunnelNetworks, strings.ToUpper, func(w *wizState, s string) { w.In.Settings.TunnelNetwork = s })
			}},
		{key: "ftls", title: T("دامنه FakeTLS"),
			when: func(w *wizState) bool { return w.p() == inbounds.MTProto },
			hint: func(w *wizState) string {
				return T("دامنه سایتی که ترافیک شبیه آن دیده شود را بفرستید.")
			},
			show: func(w *wizState) string { return w.In.Settings.FakeTLSDomain },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"www.cloudflare.com", func(w *wizState) { w.In.Settings.FakeTLSDomain = "www.cloudflare.com" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				w.In.Settings.FakeTLSDomain = strings.ToLower(text)
				return ""
			}},
		// Security.
		{key: "sec", title: T("امنیت"), edit: true,
			when: func(w *wizState) bool { return len(inbounds.SecuritiesFor(w.p(), w.In.Network)) > 1 },
			hint: func(w *wizState) string {
				if w.v2() && !inbounds.RealityAllowed(w.p(), w.In.Network) {
					return T("Reality فقط برای VLESS و Trojan روی tcp، grpc و xhttp است.")
				}
				return ""
			},
			show: func(w *wizState) string { return secLabel(w.p(), w.In.Security) },
			ok:   func(w *wizState) bool { return containsS(inbounds.SecuritiesFor(w.p(), w.In.Network), w.In.Security) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Security, w.done("sec"), inbounds.SecuritiesFor(w.p(), w.In.Network), func(s string) string { return secLabel(w.p(), s) }, func(w *wizState, s string) {
					w.In.Security = s
					if s == "tls" {
						w.tls()
					}
					if s == "reality" {
						w.reality()
					}
				})
			}},
		{key: "cert", title: T("گواهی TLS"), edit: true,
			when: func(w *wizState) bool { return w.isTLS() },
			hint: func(w *wizState) string {
				switch {
				case w.p() == inbounds.Naive:
					return T("کلاینت های NaiveProxy گواهی خودامضا را نمی پذیرند. اگر گواهی ای نیست، اول در پنل وب، بخش گواهی ها، یک گواهی Let's Encrypt بگیرید.")
				case inbounds.TLSRequired(w.p()):
					return T("بدون گواهی، پنل یک گواهی خودامضا برای SNI می سازد (کلاینت ها باید گواهی ناامن را بپذیرند).")
				}
				return T("یک گواهی را انتخاب کنید، یا یک گواهی خودامضا بسازید (کلاینت ها باید گواهی ناامن را بپذیرند).")
			},
			show: func(w *wizState) string {
				if w.In.TLS == nil || w.In.TLS.CertID == nil {
					return T("خودامضای خودکار")
				}
				return dash(w.CertName)
			},
			ok: func(w *wizState) bool {
				return w.Self || (w.In.TLS != nil && w.In.TLS.CertID != nil) || (inbounds.TLSRequired(w.p()) && w.p() != inbounds.Naive)
			},
			opts: func(r *req, w *wizState) []wopt {
				var out []wopt
				var cur int64
				if w.In.TLS != nil && w.In.TLS.CertID != nil {
					cur = *w.In.TLS.CertID
				}
				for _, c := range r.certs() {
					c := c
					if w.p() == inbounds.Naive && c.Source == "self" {
						continue
					}
					label := "🔒 " + cut(c.Domain, 30)
					if c.Source == "self" {
						label = "🧪 " + cut(c.Domain, 30)
					}
					out = append(out, wopt{label: label, on: w.done("cert") && cur == c.ID, set: func(_ *req, w *wizState) {
						id := c.ID
						w.tls().CertID, w.CertName, w.Self = &id, c.Domain, false
					}})
				}
				switch {
				case w.p() == inbounds.Naive:
				case inbounds.TLSRequired(w.p()):
					out = append(out, wopt{label: T("🧪 خودامضای خودکار"), on: w.done("cert") && cur == 0, set: func(_ *req, w *wizState) {
						w.tls().CertID, w.CertName, w.Self = nil, "", false
					}})
				default:
					out = append(out, wopt{label: T("🧪 ساختن گواهی خودامضا"), set: func(_ *req, w *wizState) {
						w.Self = true
						w.unmark("selfdom")
					}})
				}
				return out
			}},
		{key: "selfdom", title: T("دامنه گواهی خودامضا"), edit: true,
			when: func(w *wizState) bool { return w.isTLS() && w.Self },
			hint: func(w *wizState) string {
				return T("نام دامنه یا IP گواهی را بفرستید (همان که در SNI کلاینت ها می آید).")
			},
			show: func(w *wizState) string { return "" },
			opts: func(*req, *wizState) []wopt {
				return []wopt{{label: T("پیشنهاد: ") + "www.bing.com", set: func(r *req, w *wizState) { wizSelfSigned(r, w, "www.bing.com") }}}
			},
			input: func(r *req, w *wizState, text string) string {
				return wizSelfSigned(r, w, text)
			}},
		{key: "sni", title: "SNI", edit: true,
			when: func(w *wizState) bool { return w.isTLS() },
			hint: func(w *wizState) string {
				if inbounds.TLSRequired(w.p()) && (w.In.TLS == nil || w.In.TLS.CertID == nil) {
					return T("نام سروری که کلاینت ها می فرستند؛ گواهی خودامضا هم با همین نام ساخته می شود. خالی یعنی www.bing.com.")
				}
				return T("نام سروری که کلاینت ها در دست دهی TLS می فرستند را بفرستید.")
			},
			show: func(w *wizState) string { return dash(w.tls().SNI) },
			opts: func(_ *req, w *wizState) []wopt {
				var out []wopt
				if w.CertName != "" {
					d := w.CertName
					out = append(out, sugg(T("دامنه گواهی: ")+d, func(w *wizState) { w.tls().SNI = d }))
				}
				return append(out, sugg(T("خالی"), func(w *wizState) { w.tls().SNI = "" }))
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " /") {
					return T("⚠️ فقط نام دامنه بفرستید.")
				}
				w.tls().SNI = text
				return ""
			}},
		{key: "fp", title: T("اثر انگشت کلاینت (fingerprint)"), edit: true,
			when: func(w *wizState) bool {
				return w.isTLS() && (w.v2() || (inbounds.TLSRequired(w.p()) && w.p() != inbounds.AnyTLS && w.p() != inbounds.Naive))
			},
			show: func(w *wizState) string { return w.tls().Fingerprint },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.tls().Fingerprint, w.done("fp"), inbounds.TLSFingerprints, same, func(w *wizState, s string) { w.tls().Fingerprint = s })
			}},
		{key: "alpn", title: "ALPN", edit: true, multi: true,
			when: func(w *wizState) bool { return w.isTLS() && (w.v2() || w.p() == inbounds.Masque) },
			hint: func(w *wizState) string {
				if w.p() == inbounds.Masque {
					return T("h3 یعنی QUIC و h2 یعنی HTTP/2 روی TCP؛ حداقل یکی. بعد تأیید را بزنید.")
				}
				return T("مقدار ها را روشن یا خاموش کنید و بعد تأیید را بزنید (هیچ کدام یعنی بدون ALPN).")
			},
			show: func(w *wizState) string { return dash(joinList(w.tls().ALPN)) },
			ok: func(w *wizState) bool {
				return w.p() != inbounds.Masque || len(w.tls().ALPN) > 0
			},
			opts: func(_ *req, w *wizState) []wopt {
				vals := inbounds.TLSALPNs
				if w.p() == inbounds.Masque {
					vals = inbounds.MasqueALPNs
				}
				var out []wopt
				for _, a := range vals {
					a := a
					on := containsS(w.tls().ALPN, a)
					out = append(out, wopt{label: onOff(on) + " " + a, set: func(_ *req, w *wizState) {
						t := w.tls()
						var l []string
						for _, x := range vals {
							if (x == a) != containsS(t.ALPN, x) {
								l = append(l, x)
							}
						}
						t.ALPN = l
					}})
				}
				return out
			}},
		{key: "insecure", title: T("اجازه گواهی ناامن (allowInsecure)"), edit: true,
			when: func(w *wizState) bool {
				return w.isTLS() && (w.v2() || (inbounds.TLSRequired(w.p()) && w.p() != inbounds.Naive && w.In.TLS != nil && w.In.TLS.CertID != nil))
			},
			hint: func(w *wizState) string {
				return T("برای گواهی خودامضا روشن کنید تا کلاینت ها آن را بپذیرند.")
			},
			show: func(w *wizState) string { return onText(w.tls().AllowInsecure) },
			opts: func(_ *req, w *wizState) []wopt {
				return boolOpts(w.tls().AllowInsecure, w.done("insecure"), func(w *wizState, b bool) { w.tls().AllowInsecure = b })
			}},
		{key: "dest", title: T("مقصد Reality (dest)"), edit: true,
			when: func(w *wizState) bool { return w.isReality() },
			hint: func(w *wizState) string {
				return T("سایت واقعی ای که Reality دست دهی آن را نشان می دهد، به شکل دامنه یا دامنه:پورت.")
			},
			show: func(w *wizState) string { return w.reality().Dest },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"www.microsoft.com:443", func(w *wizState) { w.reality().Dest = "www.microsoft.com:443" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " /") {
					return T("⚠️ نمونه درست: www.microsoft.com:443")
				}
				if _, _, err := net.SplitHostPort(text); err != nil {
					text = net.JoinHostPort(text, "443")
				}
				w.reality().Dest = text
				return ""
			}},
		{key: "sn", title: T("نام های سرور (serverNames)"), edit: true,
			when: func(w *wizState) bool { return w.isReality() },
			hint: func(w *wizState) string {
				return T("نام هایی که کلاینت ها در SNI می فرستند را با ویرگول جدا بفرستید.")
			},
			show: func(w *wizState) string { return dash(joinList(w.reality().ServerNames)) },
			opts: func(_ *req, w *wizState) []wopt {
				h, _, err := net.SplitHostPort(w.reality().Dest)
				if err != nil || h == "" || net.ParseIP(h) != nil {
					return nil
				}
				return []wopt{sugg(T("نام مقصد: ")+h, func(w *wizState) { w.reality().ServerNames = []string{h} })}
			},
			input: func(_ *req, w *wizState, text string) string {
				l := wizList(text)
				if len(l) == 0 {
					return T("⚠️ حداقل یک نام بفرستید.")
				}
				w.reality().ServerNames = l
				return ""
			}},
		{key: "sid", title: T("Short ID ها"), edit: true,
			when: func(w *wizState) bool { return w.isReality() },
			hint: func(w *wizState) string {
				return T("Short ID ها (هگز با طول زوج تا ۱۶) را با ویرگول جدا بفرستید، یا بگذارید پنل بسازد.")
			},
			show: func(w *wizState) string {
				if len(w.reality().ShortIDs) == 0 {
					return T("خودکار")
				}
				return joinList(w.reality().ShortIDs)
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("خودکار (پنل می سازد)"), func(w *wizState) { w.reality().ShortIDs = []string{} })}
			},
			input: func(_ *req, w *wizState, text string) string {
				l := wizList(strings.ToLower(text))
				for _, s := range l {
					if _, err := hex.DecodeString(s); err != nil || len(s) > 16 {
						return T("⚠️ هر Short ID هگز با طول زوج تا ۱۶ باشد.")
					}
				}
				w.reality().ShortIDs = l
				return ""
			}},
		{key: "rfp", title: T("اثر انگشت کلاینت (fingerprint)"), edit: true,
			when: func(w *wizState) bool { return w.isReality() },
			hint: func(w *wizState) string {
				return T("Reality فقط با این اثر انگشت ها کار می کند.")
			},
			show: func(w *wizState) string { return w.reality().Fingerprint },
			ok:   func(w *wizState) bool { return containsS(inbounds.RealityFingerprints, w.reality().Fingerprint) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.reality().Fingerprint, w.done("rfp"), inbounds.RealityFingerprints, same, func(w *wizState, s string) { w.reality().Fingerprint = s })
			}},
		{key: "spx", title: "SpiderX", edit: true,
			when: func(w *wizState) bool { return w.isReality() },
			hint: func(w *wizState) string {
				return T("مسیر آغازین خزنده کلاینت (با / شروع می شود).")
			},
			show: func(w *wizState) string { return w.reality().SpiderX },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+"/", func(w *wizState) { w.reality().SpiderX = "/" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if !strings.HasPrefix(text, "/") {
					text = "/" + text
				}
				w.reality().SpiderX = text
				return ""
			}},
		// VLESS Encryption.
		{key: "enc", title: T("رمزگذاری VLESS"),
			when: func(w *wizState) bool { return w.p() == inbounds.VLESS },
			hint: func(w *wizState) string {
				return T("لایه رمزگذاری پساکوانتومی خود VLESS (Xray 25.9.5 به بالا در سرور و کلاینت).")
			},
			show: func(w *wizState) string { return onText(w.encOn()) },
			opts: func(_ *req, w *wizState) []wopt {
				return boolOpts(w.encOn(), w.done("enc"), func(w *wizState, b bool) { w.enc().Enabled = b })
			}},
		{key: "eauth", title: T("کلید سرور (رمزگذاری VLESS)"),
			when: func(w *wizState) bool { return w.p() == inbounds.VLESS && w.encOn() },
			show: func(w *wizState) string { return encAuthLabel(w.enc().Auth) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.enc().Auth, w.done("eauth"), inbounds.EncAuths, encAuthLabel, func(w *wizState, s string) { w.enc().Auth = s })
			}},
		{key: "emode", title: T("ظاهر ترافیک (رمزگذاری VLESS)"),
			when: func(w *wizState) bool { return w.p() == inbounds.VLESS && w.encOn() },
			show: func(w *wizState) string { return w.enc().Mode },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.enc().Mode, w.done("emode"), inbounds.EncModes, same, func(w *wizState, s string) { w.enc().Mode = s })
			}},
		{key: "ertt", title: T("دست دهی (رمزگذاری VLESS)"),
			when: func(w *wizState) bool { return w.p() == inbounds.VLESS && w.encOn() },
			show: func(w *wizState) string { return w.enc().RTT },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.enc().RTT, w.done("ertt"), inbounds.EncRTTs, func(s string) string {
					if s == "0rtt" {
						return "0-RTT"
					}
					return "1-RTT"
				}, func(w *wizState, s string) { w.enc().RTT = s })
			}},
		{key: "eticket", title: T("عمر تیکت 0-RTT (ثانیه)"),
			when: func(w *wizState) bool { return w.p() == inbounds.VLESS && w.encOn() && w.enc().RTT == "0rtt" },
			hint: func(w *wizState) string { return T("عددی از ۱ تا ۸۶۴۰۰ بفرستید.") },
			show: func(w *wizState) string { return fa(strconv.Itoa(w.enc().TicketSeconds)) },
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("پیش فرض: ")+fa("600"), func(w *wizState) { w.enc().TicketSeconds = 600 })}
			},
			input: func(_ *req, w *wizState, text string) string {
				return numIn(text, 1, 86400, func(n int) { w.enc().TicketSeconds = n })
			}},
		{key: "flow", title: "Flow", edit: true,
			when: func(w *wizState) bool {
				return inbounds.FlowAllowed(w.p(), w.In.Network, w.In.Security, w.encOn())
			},
			show: func(w *wizState) string { return dash(w.In.Flow) },
			opts: func(_ *req, w *wizState) []wopt {
				return choice(w.In.Flow, w.done("flow"), inbounds.Flows, func(s string) string {
					if s == "" {
						return "none"
					}
					return s
				}, func(w *wizState, s string) { w.In.Flow = s })
			}},
		// Common.
		{key: "sniff", title: T("شنود (sniffing)"),
			when: func(w *wizState) bool { return inbounds.SniffingApplies(w.p()) },
			hint: func(w *wizState) string {
				return T("دامنه مقصد از ترافیک خوانده شود (برای قانون های مسیریابی و گزارش ها).")
			},
			show: func(w *wizState) string { return onText(w.In.Sniffing) },
			opts: func(_ *req, w *wizState) []wopt {
				return boolOpts(w.In.Sniffing, w.done("sniff"), func(w *wizState, b bool) { w.In.Sniffing = b })
			}},
		{key: "addr", title: T("آدرس در لینک ها"),
			when: func(w *wizState) bool { return inbounds.HasUsers(w.p()) },
			hint: func(w *wizState) string {
				return T("آدرس یا دامنه ای که در لینک های کاربران نوشته شود.")
			},
			show: func(w *wizState) string {
				if w.In.AddressOverride == "" {
					return T("آدرس پیش فرض سرور")
				}
				return w.In.AddressOverride
			},
			opts: func(*req, *wizState) []wopt {
				return []wopt{sugg(T("آدرس پیش فرض سرور"), func(w *wizState) { w.In.AddressOverride = "" })}
			},
			input: func(_ *req, w *wizState, text string) string {
				if strings.ContainsAny(text, " /") {
					return T("⚠️ فقط IP یا دامنه بفرستید.")
				}
				w.In.AddressOverride = text
				return ""
			}},
		{key: "place", title: T("محل اجرا"), multi: true,
			when: func(w *wizState) bool { return inbounds.NodeCapable(w.p()) && len(w.Nodes) > 0 },
			hint: func(w *wizState) string {
				return T("سرور هایی که این ورودی روی آن ها اجرا شود را روشن کنید و بعد تأیید را بزنید.")
			},
			show: func(w *wizState) string {
				var parts []string
				if w.In.ServeMain {
					parts = append(parts, T("سرور اصلی"))
				}
				for _, id := range w.In.NodeIDs {
					for _, n := range w.Nodes {
						if n.ID == id {
							parts = append(parts, T("نود ")+n.Name)
						}
					}
				}
				return dash(strings.Join(parts, T("، ")))
			},
			ok: func(w *wizState) bool { return w.In.ServeMain || len(w.In.NodeIDs) > 0 },
			opts: func(_ *req, w *wizState) []wopt {
				out := []wopt{{label: onOff(w.In.ServeMain) + T(" سرور اصلی"), set: func(_ *req, w *wizState) { w.In.ServeMain = !w.In.ServeMain }}}
				for _, n := range w.Nodes {
					n := n
					on := false
					for _, id := range w.In.NodeIDs {
						on = on || id == n.ID
					}
					out = append(out, wopt{label: onOff(on) + T(" نود ") + cut(n.Name, 20), set: func(_ *req, w *wizState) {
						var ids []int64
						for _, id := range w.In.NodeIDs {
							if id != n.ID {
								ids = append(ids, id)
							}
						}
						if !on {
							ids = append(ids, n.ID)
						}
						w.In.NodeIDs = ids
					}})
				}
				return out
			}},
	}

	on("inp", wizStart)
	on("wz", func(r *req, _ []string) { wizRedraw(r, "") })
	on("wzo", wizOption)
	on("wzn", wizConfirm)
	on("wzb", wizBack)
	on("wzj", wizJump)
	on("wzc", wizFinish)
	on("wze", wizEditStart)
	onInput("wzt", wizInput)
}

func containsS(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// wizSelfSigned makes a self-signed certificate for the inbound.
func wizSelfSigned(r *req, w *wizState, domain string) string {
	var c certRow
	if err := r.call("POST", "/certs/self-signed", map[string]any{"domain": domain}, &c); err != nil {
		w.warn = "⚠️ " + esc(errText(err))
		return w.warn
	}
	id := c.ID
	w.tls().CertID, w.CertName, w.Self = &id, c.Domain, false
	w.mark("cert")
	return ""
}

// wizStart begins a new inbound of the chosen protocol.
func wizStart(r *req, a []string) {
	p := argStr(a, 0)
	if !containsS(inbounds.Protocols, p) {
		inboundsList(r, nil)
		return
	}
	w := &wizState{In: inbounds.View{Protocol: p, Enabled: true, Security: "none", NodeIDs: []int64{}}}
	if inbounds.TLSRequired(p) {
		// The only security these protocols have: nothing to choose.
		w.In.Security, w.In.TLS = "tls", &inbounds.TLS{}
	}
	if inbounds.NodeCapable(p) {
		for _, n := range r.nodes() {
			w.Nodes = append(w.Nodes, wizNode{ID: n.ID, Name: n.Name})
		}
	}
	if len(w.Nodes) == 0 {
		w.In.ServeMain = true
	}
	r.st.draft = map[string]string{}
	wizNext(r, w)
}

// wizEditStart edits the security and transport of an inbound.
func wizEditStart(r *req, a []string) {
	id := argInt(a, 0)
	var v inbounds.View
	if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &v); err != nil {
		r.fail(err, "il")
		return
	}
	w := &wizState{Edit: id, In: v}
	if v.TLS != nil && v.TLS.CertID != nil {
		for _, c := range r.certs() {
			if c.ID == *v.TLS.CertID {
				w.CertName = c.Domain
			}
		}
	}
	for i := range wizFields {
		if w.applies(&wizFields[i]) {
			w.Done = append(w.Done, wizFields[i].key)
		}
	}
	w.Cur = ""
	wizSave(r, w)
	wizDraw(r, w, "")
}

// wizNext shows the first question still to answer, or the review.
func wizNext(r *req, w *wizState) {
	wizFix(w)
	w.Cur = ""
	for i := range wizFields {
		f := &wizFields[i]
		if w.applies(f) && !w.done(f.key) {
			w.Cur = f.key
			break
		}
	}
	wizSave(r, w)
	wizDraw(r, w, "")
}

// wizFix asks again the answers a later change made invalid.
func wizFix(w *wizState) {
	for i := range wizFields {
		f := &wizFields[i]
		if f.ok != nil && w.done(f.key) && w.applies(f) && !f.ok(w) {
			w.unmark(f.key)
		}
	}
}

func wizRedraw(r *req, warn string) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	wizDraw(r, w, warn)
}

func (w *wizState) cancel() string {
	if w.Edit != 0 {
		return cbd("iv", w.Edit)
	}
	return "il"
}

func (w *wizState) title() string {
	if w.Edit != 0 {
		return header("🔧", T("امنیت و انتقال · ")+w.In.Remark)
	}
	return header("➕", T("ورودی جدید · ")+protoLabel(w.p()))
}

func wizDraw(r *req, w *wizState, warn string) {
	f := wizField(w.Cur)
	if f == nil || !w.applies(f) {
		wizReview(r, w, warn)
		return
	}
	step := 1
	for i := range wizFields {
		g := &wizFields[i]
		if g.key != f.key && w.done(g.key) && w.applies(g) {
			step++
		}
	}
	t := w.title() + Tf("مرحله %s · ", fa(strconv.Itoa(step))) + "<b>" + esc(f.title) + "</b>\n"
	if f.hint != nil {
		if h := f.hint(w); h != "" {
			t += h + "\n"
		}
	}
	if (w.done(f.key) || f.multi) && f.show != nil {
		if s := f.show(w); s != "" {
			t += T("فعلی: ") + code(s) + "\n"
		}
	}
	if f.input != nil {
		t += T("\n✍️ مقدار را بفرستید یا یک دکمه را بزنید.")
	}
	if warn != "" {
		t += "\n\n" + warn
	}
	var kb Keyboard
	var bs []Button
	long := false
	opts := []wopt{}
	if f.opts != nil {
		opts = f.opts(r, w)
	}
	for i, o := range opts {
		label := o.label
		if o.on {
			label = "✅ " + label
		}
		long = long || len([]rune(label)) > 18
		bs = append(bs, btn(label, "wzo", f.key, i))
	}
	if long || f.input != nil {
		kb = chunk(bs, 1)
	} else {
		kb = chunk(bs, 2)
	}
	if f.multi {
		kb = append(kb, []Button{btn(T("✔️ تأیید"), "wzn", f.key)})
	}
	kb = append(kb, []Button{btn(T("🔙 بازگشت"), "wzb", f.key), btn(T("✖️ انصراف"), w.cancel())})
	if f.input != nil {
		r.st.pending = &pending{kind: "wzt", args: []string{f.key}, back: w.cancel()}
	}
	r.show(view{text: t, kb: kb})
}

func wizReview(r *req, w *wizState, warn string) {
	t := w.title()
	if w.Edit != 0 {
		t += T("مقدار ها را بررسی کنید؛ برای تغییر هر کدام، دکمه آن را بزنید.") + "\n\n"
	} else {
		t += T("بررسی نهایی. برای تغییر هر مقدار، دکمه آن را بزنید.") + "\n\n"
		t += T("پروتکل: ") + "<b>" + protoLabel(w.p()) + "</b>\n"
	}
	var bs []Button
	for i := range wizFields {
		f := &wizFields[i]
		if !w.applies(f) || f.key == "selfdom" {
			continue
		}
		val := ""
		if f.show != nil {
			val = f.show(w)
		}
		t += "• " + esc(f.title) + ": " + code(val) + "\n"
		bs = append(bs, btn("✏️ "+cut(f.title, 22), "wzj", f.key))
	}
	if inbounds.IsSingbox(w.p()) {
		t += T("ℹ️ با sing-box و فقط روی سرور اصلی اجرا می شود.") + "\n"
	}
	if warn != "" {
		t += "\n" + warn
	}
	kb := chunk(bs, 2)
	if w.Edit != 0 {
		kb = append(kb, []Button{btn(T("💾 ذخیره"), "wzc")})
	} else {
		kb = append(kb, []Button{btn(T("✅ ساختن ورودی"), "wzc")})
	}
	kb = append(kb, []Button{btn(T("🔙 بازگشت"), "wzb"), btn(T("✖️ انصراف"), w.cancel())})
	r.show(view{text: t, kb: kb})
}

// wizOption handles a button of the question on screen.
func wizOption(r *req, a []string) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	f := wizField(argStr(a, 0))
	if f == nil || f.key != w.Cur || f.opts == nil {
		wizDraw(r, w, "")
		return
	}
	opts := f.opts(r, w)
	i := int(argInt(a, 1))
	if i < 0 || i >= len(opts) {
		wizDraw(r, w, "")
		return
	}
	opts[i].set(r, w)
	if f.multi || w.warn != "" {
		wizSave(r, w)
		wizDraw(r, w, w.warn)
		return
	}
	w.mark(f.key)
	wizNext(r, w)
}

// wizConfirm moves on from a question with switches.
func wizConfirm(r *req, a []string) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	f := wizField(argStr(a, 0))
	if f == nil || f.key != w.Cur {
		wizDraw(r, w, "")
		return
	}
	if f.ok != nil && !f.ok(w) {
		r.alert(T("حداقل یکی را انتخاب کنید."))
		wizDraw(r, w, "")
		return
	}
	w.mark(f.key)
	wizNext(r, w)
}

func wizInput(r *req, a []string, text string, _ *Document) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	f := wizField(argStr(a, 0))
	if f == nil || f.key != w.Cur || f.input == nil {
		wizDraw(r, w, "")
		return
	}
	if warn := f.input(r, w, strings.TrimSpace(text)); warn != "" {
		wizSave(r, w)
		wizDraw(r, w, warn)
		return
	}
	w.mark(f.key)
	wizNext(r, w)
}

// wizBack shows the previous answered question with its answer; from the
// first one back to the protocol list (new) or the review (editing), and
// from the review of an edit back to the inbound.
func wizBack(r *req, _ []string) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	if w.Edit != 0 && w.Cur == "" {
		r.st.draft = map[string]string{}
		inboundView(r, []string{strconv.FormatInt(w.Edit, 10)})
		return
	}
	for i := len(w.Done) - 1; i >= 0; i-- {
		k := w.Done[i]
		f := wizField(k)
		if k == w.Cur || f == nil || !w.applies(f) {
			continue
		}
		if f.key == "cert" {
			w.Self = false
		}
		w.Cur = k
		wizSave(r, w)
		wizDraw(r, w, "")
		return
	}
	if w.Edit != 0 {
		w.Cur = ""
		wizSave(r, w)
		wizDraw(r, w, "")
		return
	}
	r.st.draft = map[string]string{}
	routes["in"](r, nil)
}

// wizJump answers one question again from the review (its answer stays
// until a new one is given).
func wizJump(r *req, a []string) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	f := wizField(argStr(a, 0))
	if f == nil || !w.applies(f) {
		wizDraw(r, w, "")
		return
	}
	w.Cur = f.key
	wizSave(r, w)
	wizDraw(r, w, "")
}

// wizBody is the API body of the draft: values that do not apply are left
// out the way the web wizard does it.
func wizBody(w *wizState) map[string]any {
	v := w.In
	p := v.Protocol
	if !inbounds.IsV2(p) {
		v.Network, v.Transport, v.Flow, v.Reality = "", inbounds.Transport{}, "", nil
		if p != inbounds.ShadowTLS {
			v.SSMethod = ""
		}
	}
	if v.Security != "tls" {
		v.TLS = nil
	}
	if v.Security != "reality" {
		v.Reality = nil
	}
	if !inbounds.FlowAllowed(p, v.Network, v.Security, v.VLESSEnc != nil && v.VLESSEnc.Enabled) {
		v.Flow = ""
	}
	if p != inbounds.VLESS {
		v.VLESSEnc = nil
	}
	if !inbounds.SniffingApplies(p) {
		v.Sniffing = false
	}
	if !inbounds.HasPort(p) {
		v.Port, v.Listen = 0, ""
	}
	if v.NodeIDs == nil || !inbounds.NodeCapable(p) {
		v.NodeIDs = []int64{}
	}
	if len(v.NodeIDs) == 0 {
		v.ServeMain = true
	}
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"id", "user_count", "host_count", "on_main", "created_at", "xray_needs", "xray_too_old"} {
		delete(m, k)
	}
	return m
}

// wizFinish creates the inbound, or saves the edited security/transport.
func wizFinish(r *req, _ []string) {
	w := wizLoad(r)
	if w == nil {
		inboundsList(r, nil)
		return
	}
	if w.Cur != "" {
		wizDraw(r, w, "")
		return
	}
	body := wizBody(w)
	if w.Edit != 0 {
		keys := []string{"security", "tls"}
		if inbounds.IsV2(w.p()) {
			keys = append(keys, "network", "transport", "reality", "flow")
		}
		patch := map[string]any{}
		for _, k := range keys {
			patch[k] = body[k]
		}
		r.progress(T("در حال ذخیره"))
		if err := r.call("PATCH", fmt.Sprintf("/inbounds/%d", w.Edit), patch, nil); err != nil {
			r.fail(err, "wz")
			return
		}
		id := w.Edit
		r.st.draft = map[string]string{}
		inboundView(r, []string{strconv.FormatInt(id, 10)})
		return
	}
	r.progress(T("در حال ساختن ورودی"))
	var out inbound
	if err := r.call("POST", "/inbounds", body, &out); err != nil {
		r.fail(err, "wz")
		return
	}
	r.st.draft = map[string]string{}
	inboundView(r, []string{strconv.FormatInt(out.ID, 10)})
}
