package tgbot

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Notifications. Every NotifyEvery the bot looks for events, drops the ones
// already sent (telegram_notices: kind + subject + mark), and sends what is
// left to every bot admin as one message per round. A notice stays recorded
// while its condition holds, and is removed once it no longer does, so the
// same event can be announced again the next time it happens (a user who
// was renewed and runs out again, a node that goes down again).

// Notice kinds.
const (
	nkTraffic90  = "traffic90"
	nkTraffic100 = "traffic100"
	nkExpiring   = "expiring"
	nkExpired    = "expired"
	nkNodeOff    = "node_off"
	nkRelease    = "release"
)

// nodeOffRounds is how many checks in a row a node must be unreachable
// before it is reported (a single missed check is not news).
const nodeOffRounds = 2

// expiredWindow limits "expired" notices to users that expired recently, so
// turning the bot on does not report every user that expired long ago.
const expiredWindow = 72 * time.Hour

// maxNoticeLines caps one message; the rest is counted.
const maxNoticeLines = 25

// event is one thing to announce.
type event struct {
	kind    string
	subject int64
	mark    string
	text    string
	// user events carry the user for the buttons.
	user   *db.User
	nodeID int64
	name   string
	// record=false: nothing to store (a node came back: its node_off notice
	// is removed instead).
	record bool
}

func (b *Bot) notifyLoop(ctx context.Context, cl *Client) {
	for sleepCtx(ctx, b.NotifyEvery) {
		cfg := LoadConfig(b.DB)
		if !cfg.Enabled || len(cfg.Admins) == 0 {
			continue
		}
		func() {
			defer func() {
				if p := recover(); p != nil {
					slog.Error("telegram notifications: panic", "err", p)
				}
			}()
			rctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			b.notifyRound(rctx, cl, cfg)
		}()
	}
}

// notifyRound finds new events, sends them grouped and records them.
func (b *Bot) notifyRound(ctx context.Context, cl *Client, cfg Config) {
	n := cfg.Notify
	if !n.Traffic && !n.Expiry && !n.Nodes && !n.Update {
		return
	}
	now := b.now()
	var evs []event
	// active[kind] = subjects whose condition holds now; notices of those
	// kinds for other subjects are removed after the round.
	active := map[string]map[int64]bool{}
	if n.Traffic || n.Expiry {
		evs = append(evs, b.userEvents(now, n, active)...)
	}
	var back []event
	if n.Nodes {
		var off []event
		off, back = b.nodeEvents(ctx, cfg, active)
		evs = append(evs, off...)
	}
	if n.Update {
		evs = append(evs, b.releaseEvents(ctx, cfg)...)
	}
	fresh := b.unsent(evs)
	fresh = append(fresh, back...)
	if len(fresh) > 0 {
		text, kb := noticeMessage(fresh)
		sent := false
		for _, id := range cfg.Admins {
			if _, err := cl.Send(ctx, id, text, kb); err != nil {
				slog.Warn("telegram notification not sent", "admin", id, "err", err)
				continue
			}
			sent = true
		}
		if !sent {
			return // try again next round
		}
		for _, e := range fresh {
			if e.record {
				b.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&db.TelegramNotice{Kind: e.kind, SubjectID: e.subject, Mark: e.mark, CreatedAt: now})
			} else if e.kind == "node_on" {
				b.DB.Where("kind = ? AND subject_id = ?", nkNodeOff, e.subject).Delete(&db.TelegramNotice{})
			}
		}
	}
	b.prune(active)
}

// unsent drops events already recorded.
func (b *Bot) unsent(evs []event) []event {
	var out []event
	for _, e := range evs {
		var n int64
		b.DB.Model(&db.TelegramNotice{}).Where("kind = ? AND subject_id = ? AND mark = ?", e.kind, e.subject, e.mark).Count(&n)
		if n == 0 {
			out = append(out, e)
		}
	}
	return out
}

// prune removes notices whose condition no longer holds.
func (b *Bot) prune(active map[string]map[int64]bool) {
	for kind, subs := range active {
		var rows []db.TelegramNotice
		b.DB.Where("kind = ?", kind).Find(&rows)
		for _, r := range rows {
			if !subs[r.SubjectID] {
				b.DB.Delete(&db.TelegramNotice{}, r.ID)
			}
		}
	}
}

func (b *Bot) userEvents(now time.Time, n Notify, active map[string]map[int64]bool) []event {
	var evs []event
	if n.Traffic {
		active[nkTraffic90], active[nkTraffic100] = map[int64]bool{}, map[int64]bool{}
		var us []db.User
		b.DB.Where("data_limit > 0 AND status <> ? AND (up + down) * 10 >= data_limit * 9", db.StatusDisabled).Order("id").Find(&us)
		for i := range us {
			u := &us[i]
			used := u.Used()
			mark := strconv.FormatInt(u.DataLimit, 10)
			if used >= u.DataLimit {
				active[nkTraffic100][u.ID] = true
				// a user that went straight past 100% gets one notice
				active[nkTraffic90][u.ID] = true
				evs = append(evs, event{kind: nkTraffic100, subject: u.ID, mark: mark, user: u, record: true,
					text: "<b>" + esc(u.Username) + "</b> حجمش تمام شد (" + size(used) + " از " + size(u.DataLimit) + ")"})
				continue
			}
			active[nkTraffic90][u.ID] = true
			pct := used * 100 / u.DataLimit
			evs = append(evs, event{kind: nkTraffic90, subject: u.ID, mark: mark, user: u, record: true,
				text: "<b>" + esc(u.Username) + "</b> " + faInt(pct) + "٪ حجمش را مصرف کرد (" + size(used) + " از " + size(u.DataLimit) + ")"})
		}
	}
	if n.Expiry {
		active[nkExpiring], active[nkExpired] = map[int64]bool{}, map[int64]bool{}
		var us []db.User
		b.DB.Where("expire_at IS NOT NULL AND expire_at > ? AND expire_at <= ? AND status <> ?", now.Add(-expiredWindow), now.Add(24*time.Hour), db.StatusDisabled).
			Order("expire_at").Find(&us)
		for i := range us {
			u := &us[i]
			mark := strconv.FormatInt(u.ExpireAt.Unix(), 10)
			if !u.ExpireAt.After(now) {
				active[nkExpired][u.ID] = true
				active[nkExpiring][u.ID] = true
				evs = append(evs, event{kind: nkExpired, subject: u.ID, mark: mark, user: u, record: true,
					text: "<b>" + esc(u.Username) + "</b> منقضی شد"})
				continue
			}
			active[nkExpiring][u.ID] = true
			evs = append(evs, event{kind: nkExpiring, subject: u.ID, mark: mark, user: u, record: true,
				text: "<b>" + esc(u.Username) + "</b> کمتر از یک روز مانده (" + left(u.ExpireAt.Sub(now)) + ")"})
		}
	}
	// A user who already got the 100% (or expired) notice does not also get
	// the 90% (or expiring) one in the same round.
	return dropShadowed(evs)
}

func dropShadowed(evs []event) []event {
	full := map[string]bool{}
	for _, e := range evs {
		if e.kind == nkTraffic100 || e.kind == nkExpired {
			full[e.kind+":"+strconv.FormatInt(e.subject, 10)] = true
		}
	}
	var out []event
	for _, e := range evs {
		id := strconv.FormatInt(e.subject, 10)
		if (e.kind == nkTraffic90 && full[nkTraffic100+":"+id]) || (e.kind == nkExpiring && full[nkExpired+":"+id]) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (b *Bot) nodeEvents(ctx context.Context, cfg Config, active map[string]map[int64]bool) (off, back []event) {
	var ns []nodeRow
	if err := b.call(ctx, cfg.Admins[0], "GET", "/nodes", nil, &ns); err != nil {
		return nil, nil
	}
	active[nkNodeOff] = map[int64]bool{}
	var known []db.TelegramNotice
	b.DB.Where("kind = ?", nkNodeOff).Find(&known)
	reported := map[int64]bool{}
	for _, k := range known {
		reported[k.SubjectID] = true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[int64]bool{}
	for i := range ns {
		nd := &ns[i]
		seen[nd.ID] = true
		st := nd.State.Status
		down := nd.Enabled && st != "online" && st != "degraded" && st != "waiting" && st != "disabled"
		if !down {
			b.nodeOff[nd.ID] = 0
			if reported[nd.ID] && nd.Enabled && (st == "online" || st == "degraded") {
				back = append(back, event{kind: "node_on", subject: nd.ID, nodeID: nd.ID, name: nd.Name,
					text: "🟢 <b>" + esc(nd.Name) + "</b> دوباره وصل شد"})
				active[nkNodeOff][nd.ID] = true // removed after the send
			} else if reported[nd.ID] && nd.Enabled {
				active[nkNodeOff][nd.ID] = true // not back yet (waiting)
			}
			continue
		}
		b.nodeOff[nd.ID]++
		if b.nodeOff[nd.ID] < nodeOffRounds && !reported[nd.ID] {
			continue
		}
		active[nkNodeOff][nd.ID] = true
		msg := "🔴 <b>" + esc(nd.Name) + "</b> آفلاین شد"
		if nd.State.Error != "" {
			msg += " · " + esc(cut(nd.State.Error, 80))
		}
		off = append(off, event{kind: nkNodeOff, subject: nd.ID, mark: "1", nodeID: nd.ID, name: nd.Name, record: true, text: msg})
	}
	for id := range b.nodeOff {
		if !seen[id] {
			delete(b.nodeOff, id)
		}
	}
	return off, back
}

func (b *Bot) releaseEvents(ctx context.Context, cfg Config) []event {
	var info updInfo
	if err := b.call(ctx, cfg.Admins[0], "GET", "/system/update", nil, &info); err != nil || info.Available == nil || info.Available.Tag == "" {
		return nil
	}
	if info.Available.Tag == info.Current {
		return nil
	}
	return []event{{kind: nkRelease, mark: info.Available.Tag, record: true,
		text: "نسخه تازه پنل منتشر شد: " + code(info.Available.Tag) + " (نسخه فعلی " + code(info.Current) + ")"}}
}

// noticeMessage groups a round's events into one message with buttons.
func noticeMessage(evs []event) (string, Keyboard) {
	groups := []struct {
		title string
		kinds []string
	}{
		{"📊 حجم", []string{nkTraffic100, nkTraffic90}},
		{"⏳ انقضا", []string{nkExpired, nkExpiring}},
		{"🌐 نود ها", []string{nkNodeOff, "node_on"}},
		{"⬆️ بروزرسانی", []string{nkRelease}},
	}
	t := "🔔 <b>Veilon · اعلان ها</b>\n" + line + "\n"
	lines := 0
	hidden := 0
	for _, g := range groups {
		var part []string
		for _, k := range g.kinds {
			for _, e := range evs {
				if e.kind != k {
					continue
				}
				if lines >= maxNoticeLines {
					hidden++
					continue
				}
				lines++
				part = append(part, "• "+e.text)
			}
		}
		if len(part) > 0 {
			t += "\n<b>" + g.title + "</b>\n" + strings.Join(part, "\n") + "\n"
		}
	}
	if hidden > 0 {
		t += "\n… و " + faInt(hidden) + " مورد دیگر"
	}
	// Buttons open a fresh menu below, so the notice itself stays in the chat.
	var kb Keyboard
	users := map[int64]bool{}
	nodes := map[int64]bool{}
	release := false
	for _, e := range evs {
		switch {
		case e.user != nil && !users[e.user.ID] && len(users) < 8:
			users[e.user.ID] = true
			row := []Button{btn("👤 "+cut(e.user.Username, 18), "nx", "uv", e.user.ID)}
			if e.user.ExpireAt != nil {
				row = append(row, btn("➕ ۳۰ روز", "nx", "nren", e.user.ID))
			} else {
				row = append(row, btn("♻️ تمدید", "nx", "ur", e.user.ID))
			}
			kb = append(kb, row)
		case e.nodeID != 0 && !nodes[e.nodeID] && len(nodes) < 4:
			nodes[e.nodeID] = true
			kb = append(kb, []Button{btn("🌐 "+cut(e.name, 24), "nx", "nv", e.nodeID)})
		case e.kind == nkRelease && !release:
			release = true
			kb = append(kb, []Button{btn("⬆️ بروزرسانی پنل", "nx", "upd")})
		}
	}
	return t, kb
}

func init() {
	// nx:<key>:<args>: a button on a notice; the screen opens as a new
	// message so the notice stays readable.
	on("nx", func(r *req, a []string) {
		if len(a) == 0 {
			home(r, nil)
			return
		}
		r.msgID = 0
		h := routes[a[0]]
		if h == nil || a[0] == "nx" {
			home(r, nil)
			return
		}
		h(r, a[1:])
	})
	on("nren", func(r *req, a []string) {
		id := argInt(a, 0)
		u, err := r.getUser(id)
		if err != nil {
			r.fail(err, "home")
			return
		}
		q := "۳۰ روز به زمان <b>" + esc(u.Username) + "</b> اضافه شود؟"
		if u.ExpireAt != nil {
			q += "\nانقضای فعلی: " + jdate(*u.ExpireAt, r.b.loc())
		}
		confirm(r, "تمدید "+u.Username, q, cbd("ura", id, 30), cbd("uv", id))
	})
}
