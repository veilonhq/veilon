package users

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
)

func kbpsToMbps(k int64) float64 { return math.Round(float64(k)/10) / 100 }

func mbpsToKbps(m float64) int64 { return int64(math.Round(m * 1000)) }

func (s *Service) loc() *time.Location {
	return LoadLocation(db.GetSetting(s.DB, db.KeyTimezone))
}

// suspend recomputes the window / daily-cap suspension right after an edit
// so a change takes effect without waiting for the minute job. loc is read
// with s.loc() before any transaction: on SQLite the pool has a single
// connection, so reading through s.DB inside a transaction would wait for
// itself forever.
func (s *Service) suspend(u *db.User, now time.Time, loc *time.Location) {
	if IsServed(u.Status) {
		u.SuspendReason = Suspension(u, now, loc)
	} else {
		u.SuspendReason = ""
	}
}

// applyLimits validates and copies the limit fields of in to u.
func applyLimits(u *db.User, in *Input) error {
	if in.IPLimit.Set {
		if in.IPLimit.Val < 0 || in.IPLimit.Val > 1000 {
			return verr("ip_limit must be 0-1000")
		}
		u.IPLimit = in.IPLimit.Val
	}
	if in.DeviceLimit.Set {
		if in.DeviceLimit.Val < 0 || in.DeviceLimit.Val > 1000 {
			return verr("device_limit must be 0-1000")
		}
		u.DeviceLimit = in.DeviceLimit.Val
	}
	if in.RequireHWID.Set {
		u.RequireHWID = in.RequireHWID.Val
	}
	for _, f := range []struct {
		o   util.Opt[float64]
		dst *int64
		n   string
	}{{in.SpeedLimitDown, &u.SpeedDownKbps, "speed_limit_down"}, {in.SpeedLimitUp, &u.SpeedUpKbps, "speed_limit_up"}} {
		if !f.o.Set {
			continue
		}
		if f.o.Null || f.o.Val == 0 {
			*f.dst = 0
			continue
		}
		if f.o.Val < 0.1 || f.o.Val > 100000 {
			return verr("%s must be 0 (unlimited) or 0.1-100000 Mbps", f.n)
		}
		*f.dst = mbpsToKbps(f.o.Val)
	}
	if in.TrafficMultiplier.Set {
		m := in.TrafficMultiplier.Val
		if in.TrafficMultiplier.Null || m == 0 {
			m = 1
		}
		m = math.Round(m*10) / 10
		if m < 0.1 || m > 100 {
			return verr("traffic_multiplier must be 0.1-100 (step 0.1)")
		}
		u.TrafficMultiplier = m
	}
	if u.TrafficMultiplier <= 0 {
		u.TrafficMultiplier = 1
	}
	if in.DailyLimit.Set {
		if in.DailyLimit.Val < 0 {
			return verr("daily_limit must be >= 0")
		}
		u.DailyLimit = in.DailyLimit.Val
	}
	if in.BlockTorrent.Set {
		u.BlockTorrent = in.BlockTorrent.Val
	}
	if in.WindowFrom.Set {
		u.WindowFrom = strings.TrimSpace(in.WindowFrom.Val)
	}
	if in.WindowTo.Set {
		u.WindowTo = strings.TrimSpace(in.WindowTo.Val)
	}
	if (u.WindowFrom == "") != (u.WindowTo == "") {
		return verr("window_from and window_to must be set together")
	}
	for _, c := range []string{u.WindowFrom, u.WindowTo} {
		if c == "" {
			continue
		}
		m, err := ParseClock(c)
		if err != nil {
			return verr("%s", err.Error())
		}
		_ = m
	}
	if u.WindowFrom != "" {
		f, _ := ParseClock(u.WindowFrom)
		t, _ := ParseClock(u.WindowTo)
		u.WindowFrom = clock(f)
		u.WindowTo = clock(t)
	}
	if in.WindowDays.Set {
		if in.WindowDays.Val < 0 || in.WindowDays.Val > 0x7f {
			return verr("window_days must be a weekday bitmask 0-127 (bit 0 = Sunday)")
		}
		u.WindowDays = in.WindowDays.Val
		if u.WindowDays == 0x7f {
			u.WindowDays = 0
		}
	}
	if in.GroupID.Set {
		if in.GroupID.Null || in.GroupID.Val == 0 {
			u.GroupID = nil
		} else {
			g := in.GroupID.Val
			u.GroupID = &g
		}
	}
	return nil
}

func clock(m int) string {
	return string([]byte{byte('0' + m/600), byte('0' + m/60%10), ':', byte('0' + m%60/10), byte('0' + m%10)})
}

func validateGroup(tx *gorm.DB, id *int64) error {
	if id == nil {
		return nil
	}
	var n int64
	tx.Model(&db.Group{}).Where("id = ?", *id).Count(&n)
	if n == 0 {
		return verr("group_id does not exist")
	}
	return nil
}

// --- groups ---------------------------------------------------------------

// GroupDefaults are the limits a group gives new members (nil = not set).
type GroupDefaults struct {
	DataLimit         *int64   `json:"data_limit,omitempty"`
	ExpireDays        *int     `json:"expire_days,omitempty"`
	OnHoldDays        *int     `json:"on_hold_days,omitempty"`
	ResetStrategy     *string  `json:"reset_strategy,omitempty"`
	IPLimit           *int     `json:"ip_limit,omitempty"`
	DeviceLimit       *int     `json:"device_limit,omitempty"`
	RequireHWID       *bool    `json:"require_hwid,omitempty"`
	SpeedLimitDown    *float64 `json:"speed_limit_down,omitempty"`
	SpeedLimitUp      *float64 `json:"speed_limit_up,omitempty"`
	TrafficMultiplier *float64 `json:"traffic_multiplier,omitempty"`
	DailyLimit        *int64   `json:"daily_limit,omitempty"`
	BlockTorrent      *bool    `json:"block_torrent,omitempty"`
	WindowFrom        *string  `json:"window_from,omitempty"`
	WindowTo          *string  `json:"window_to,omitempty"`
	WindowDays        *int     `json:"window_days,omitempty"`
}

// GroupView is the API representation of a group.
type GroupView struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	Color      string        `json:"color"`
	AdBlock    bool          `json:"ad_block"`
	InboundIDs []int64       `json:"inbound_ids"`
	Defaults   GroupDefaults `json:"defaults"`
	Users      int64         `json:"users"`
	CreatedAt  time.Time     `json:"created_at"`
}

// GroupInput is the create/patch payload of a group.
type GroupInput struct {
	Name       util.Opt[string]        `json:"name"`
	Color      util.Opt[string]        `json:"color"`
	AdBlock    util.Opt[bool]          `json:"ad_block"`
	InboundIDs util.Opt[[]int64]       `json:"inbound_ids"`
	Defaults   util.Opt[GroupDefaults] `json:"defaults"`
}

func groupView(g *db.Group, n int64) GroupView {
	v := GroupView{ID: g.ID, Name: g.Name, Color: g.Color, AdBlock: g.AdBlock, InboundIDs: []int64{}, Users: n, CreatedAt: g.CreatedAt}
	_ = json.Unmarshal([]byte(g.InboundIDs), &v.InboundIDs)
	_ = json.Unmarshal([]byte(g.Defaults), &v.Defaults)
	if v.InboundIDs == nil {
		v.InboundIDs = []int64{}
	}
	return v
}

// Groups lists groups with member counts (owner limits counts to its users).
func (s *Service) Groups(owner *int64) ([]GroupView, error) {
	var rows []db.Group
	if err := s.DB.Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	type cnt struct {
		GroupID int64
		N       int64
	}
	var cs []cnt
	q := s.DB.Model(&db.User{}).Select("group_id, COUNT(*) AS n").Where("group_id IS NOT NULL")
	if owner != nil {
		q = q.Where("admin_id = ?", *owner)
	}
	q.Group("group_id").Scan(&cs)
	by := map[int64]int64{}
	for _, c := range cs {
		by[c.GroupID] = c.N
	}
	out := make([]GroupView, 0, len(rows))
	for i := range rows {
		out = append(out, groupView(&rows[i], by[rows[i].ID]))
	}
	return out, nil
}

var colorRe = func(c string) bool {
	if c == "" {
		return true
	}
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range strings.ToLower(c[1:]) {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// SaveGroup creates (id 0) or patches a group.
func (s *Service) SaveGroup(id int64, in *GroupInput) (*GroupView, error) {
	var g db.Group
	if id > 0 {
		if err := s.DB.First(&g, id).Error; err != nil {
			return nil, ErrNotFound
		}
	}
	if in.Name.Set {
		g.Name = strings.TrimSpace(in.Name.Val)
	}
	if g.Name == "" || len([]rune(g.Name)) > 48 {
		return nil, verr("group name must be 1-48 characters")
	}
	if in.Color.Set {
		g.Color = strings.ToLower(strings.TrimSpace(in.Color.Val))
	}
	if !colorRe(g.Color) {
		return nil, verr("color must be #rrggbb")
	}
	if in.AdBlock.Set {
		g.AdBlock = in.AdBlock.Val
	}
	if in.InboundIDs.Set {
		ids, err := s.validateInbounds(s.DB, in.InboundIDs.Val)
		if err != nil {
			return nil, err
		}
		if ids == nil {
			ids = []int64{}
		}
		b, _ := json.Marshal(ids)
		g.InboundIDs = string(b)
	}
	if in.Defaults.Set {
		d := in.Defaults.Val
		// Validate the defaults by applying them to a scratch user.
		probe := &db.User{TrafficMultiplier: 1}
		pin := &Input{}
		defaultsToInput(&d, pin, time.Now())
		if err := applyLimits(probe, pin); err != nil {
			return nil, err
		}
		b, _ := json.Marshal(d)
		g.Defaults = string(b)
	}
	var n int64
	s.DB.Model(&db.Group{}).Where("name = ? AND id <> ?", g.Name, g.ID).Count(&n)
	if n > 0 {
		return nil, verr("a group with this name already exists")
	}
	if err := s.DB.Save(&g).Error; err != nil {
		return nil, err
	}
	s.changed()
	var cnt int64
	s.DB.Model(&db.User{}).Where("group_id = ?", g.ID).Count(&cnt)
	v := groupView(&g, cnt)
	return &v, nil
}

// DeleteGroup removes a group; its members keep their settings.
func (s *Service) DeleteGroup(id int64) error {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&db.Group{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Model(&db.User{}).Where("group_id = ?", id).Update("group_id", nil).Error
	})
	if err == nil {
		s.changed()
	}
	return err
}

// SetGroup assigns users to a group (0 = remove from their group).
func (s *Service) SetGroup(groupID int64, ids ...int64) error {
	var val any
	if groupID > 0 {
		if err := validateGroup(s.DB, &groupID); err != nil {
			return err
		}
		val = groupID
	}
	err := s.DB.Model(&db.User{}).Where("id IN ?", ids).Update("group_id", val).Error
	if err == nil {
		s.changed()
	}
	return err
}

func defaultsToInput(d *GroupDefaults, in *Input, now time.Time) {
	if d.DataLimit != nil && !in.DataLimit.Set {
		in.DataLimit = util.Opt[int64]{Set: true, Val: *d.DataLimit}
	}
	if d.ExpireDays != nil && *d.ExpireDays > 0 && !in.ExpireAt.Set && !in.OnHoldDays.Set {
		in.ExpireAt = util.Opt[time.Time]{Set: true, Val: now.AddDate(0, 0, *d.ExpireDays)}
	}
	if d.OnHoldDays != nil && *d.OnHoldDays > 0 && !in.OnHoldDays.Set && !in.ExpireAt.Set {
		in.OnHoldDays = util.Opt[int]{Set: true, Val: *d.OnHoldDays}
	}
	if d.ResetStrategy != nil && !in.ResetStrategy.Set {
		in.ResetStrategy = util.Opt[string]{Set: true, Val: *d.ResetStrategy}
	}
	if d.IPLimit != nil && !in.IPLimit.Set {
		in.IPLimit = util.Opt[int]{Set: true, Val: *d.IPLimit}
	}
	if d.DeviceLimit != nil && !in.DeviceLimit.Set {
		in.DeviceLimit = util.Opt[int]{Set: true, Val: *d.DeviceLimit}
	}
	if d.RequireHWID != nil && !in.RequireHWID.Set {
		in.RequireHWID = util.Opt[bool]{Set: true, Val: *d.RequireHWID}
	}
	if d.SpeedLimitDown != nil && !in.SpeedLimitDown.Set {
		in.SpeedLimitDown = util.Opt[float64]{Set: true, Val: *d.SpeedLimitDown}
	}
	if d.SpeedLimitUp != nil && !in.SpeedLimitUp.Set {
		in.SpeedLimitUp = util.Opt[float64]{Set: true, Val: *d.SpeedLimitUp}
	}
	if d.TrafficMultiplier != nil && !in.TrafficMultiplier.Set {
		in.TrafficMultiplier = util.Opt[float64]{Set: true, Val: *d.TrafficMultiplier}
	}
	if d.DailyLimit != nil && !in.DailyLimit.Set {
		in.DailyLimit = util.Opt[int64]{Set: true, Val: *d.DailyLimit}
	}
	if d.BlockTorrent != nil && !in.BlockTorrent.Set {
		in.BlockTorrent = util.Opt[bool]{Set: true, Val: *d.BlockTorrent}
	}
	if d.WindowFrom != nil && d.WindowTo != nil && !in.WindowFrom.Set && !in.WindowTo.Set {
		in.WindowFrom = util.Opt[string]{Set: true, Val: *d.WindowFrom}
		in.WindowTo = util.Opt[string]{Set: true, Val: *d.WindowTo}
	}
	if d.WindowDays != nil && !in.WindowDays.Set {
		in.WindowDays = util.Opt[int]{Set: true, Val: *d.WindowDays}
	}
}

// applyGroupDefaults fills fields the input leaves out from the group.
func (s *Service) applyGroupDefaults(in *Input, now time.Time) error {
	var g db.Group
	if err := s.DB.First(&g, in.GroupID.Val).Error; err != nil {
		return verr("group_id does not exist")
	}
	v := groupView(&g, 0)
	defaultsToInput(&v.Defaults, in, now)
	if !in.InboundIDs.Set && len(v.InboundIDs) > 0 {
		in.InboundIDs = util.Opt[[]int64]{Set: true, Val: v.InboundIDs}
	}
	return nil
}
