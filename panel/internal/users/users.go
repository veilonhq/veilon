// Package users manages proxy users.
package users

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/presence"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// IsOnline is the panel's online rule (package presence): an open
// connection on the main server's Xray or a node's, or recent traffic on
// cores that do not list connections. The admin panel and the subscription
// page share it.
func IsOnline(id int64, now time.Time) bool { return presence.Default.Online(id, now) }

// LastOnline is when the user was last seen online: the stored value, or a
// later moment the tracker saw that is not stored yet.
func LastOnline(id int64, stored *time.Time) *time.Time {
	return presence.Default.LastOnline(id, stored)
}

// View is the API representation of a user.
type View struct {
	ID            int64      `json:"id"`
	Username      string     `json:"username"`
	Status        string     `json:"status"`
	DataLimit     int64      `json:"data_limit"`
	UsedTraffic   int64      `json:"used_traffic"`
	Up            int64      `json:"up"`
	Down          int64      `json:"down"`
	ExpireAt      *time.Time `json:"expire_at"`
	OnHoldDays    *int       `json:"on_hold_days"`
	ResetStrategy string     `json:"reset_strategy"`
	Note          string     `json:"note"`
	InboundIDs    []int64    `json:"inbound_ids"`
	UUID          string     `json:"uuid"`
	Password      string     `json:"password"`
	SubToken      string     `json:"sub_token"`
	SubURL        string     `json:"sub_url"`
	Online        bool       `json:"online"`
	LastOnlineAt  *time.Time `json:"last_online_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	IPLimit           int     `json:"ip_limit"`
	DeviceLimit       int     `json:"device_limit"`
	RequireHWID       bool    `json:"require_hwid"`
	SpeedLimitDown    float64 `json:"speed_limit_down"` // Mbps, 0 = unlimited
	SpeedLimitUp      float64 `json:"speed_limit_up"`
	TrafficMultiplier float64 `json:"traffic_multiplier"`
	DailyLimit        int64   `json:"daily_limit"`
	DailyUsed         int64   `json:"daily_used"`
	BlockTorrent      bool    `json:"block_torrent"`
	WindowFrom        string  `json:"window_from"`
	WindowTo          string  `json:"window_to"`
	WindowDays        int     `json:"window_days"`
	SuspendReason     string  `json:"suspend_reason"`
	GroupID           *int64  `json:"group_id"`
	AdminID           *int64  `json:"admin_id"`
}

// Input is the create/patch payload.
type Input struct {
	Username      util.Opt[string]    `json:"username"`
	DataLimit     util.Opt[int64]     `json:"data_limit"`
	ExpireAt      util.Opt[time.Time] `json:"expire_at"`
	OnHoldDays    util.Opt[int]       `json:"on_hold_days"`
	ResetStrategy util.Opt[string]    `json:"reset_strategy"`
	Note          util.Opt[string]    `json:"note"`
	InboundIDs    util.Opt[[]int64]   `json:"inbound_ids"`
	Status        util.Opt[string]    `json:"status"`

	IPLimit           util.Opt[int]     `json:"ip_limit"`
	DeviceLimit       util.Opt[int]     `json:"device_limit"`
	RequireHWID       util.Opt[bool]    `json:"require_hwid"`
	SpeedLimitDown    util.Opt[float64] `json:"speed_limit_down"`
	SpeedLimitUp      util.Opt[float64] `json:"speed_limit_up"`
	TrafficMultiplier util.Opt[float64] `json:"traffic_multiplier"`
	DailyLimit        util.Opt[int64]   `json:"daily_limit"`
	BlockTorrent      util.Opt[bool]    `json:"block_torrent"`
	WindowFrom        util.Opt[string]  `json:"window_from"`
	WindowTo          util.Opt[string]  `json:"window_to"`
	WindowDays        util.Opt[int]     `json:"window_days"`
	GroupID           util.Opt[int64]   `json:"group_id"`
}

// Errors.
var (
	ErrNotFound = errors.New("user not found")
)

// ValidationError is returned for bad input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func verr(f string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(f, a...)} }

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,32}$`)

// Service provides user operations.
type Service struct {
	DB *gorm.DB
	// SubURL builds the subscription URL for a token; host is the request host fallback.
	SubURL   func(token, host string) string
	OnChange func()
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) changed() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

// ToView converts a model; inboundIDs may be nil.
func (s *Service) ToView(u *db.User, inboundIDs []int64, host string) View {
	if inboundIDs == nil {
		inboundIDs = []int64{}
	}
	v := View{
		ID: u.ID, Username: u.Username, Status: u.Status, DataLimit: u.DataLimit,
		UsedTraffic: u.Used(), Up: u.Up, Down: u.Down, ExpireAt: u.ExpireAt, OnHoldDays: u.OnHoldDays,
		ResetStrategy: u.ResetStrategy, Note: u.Note, InboundIDs: inboundIDs, UUID: u.UUID,
		Password: u.Password, SubToken: u.SubToken, LastOnlineAt: LastOnline(u.ID, u.LastOnlineAt),
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		IPLimit: u.IPLimit, DeviceLimit: u.DeviceLimit, RequireHWID: u.RequireHWID,
		SpeedLimitDown: kbpsToMbps(u.SpeedDownKbps), SpeedLimitUp: kbpsToMbps(u.SpeedUpKbps),
		TrafficMultiplier: u.Multiplier(), DailyLimit: u.DailyLimit, DailyUsed: u.DailyUsed,
		BlockTorrent: u.BlockTorrent, WindowFrom: u.WindowFrom, WindowTo: u.WindowTo, WindowDays: u.WindowDays,
		SuspendReason: u.SuspendReason, GroupID: u.GroupID, AdminID: u.AdminID,
	}
	v.Online = IsOnline(u.ID, s.now())
	if s.SubURL != nil {
		v.SubURL = s.SubURL(u.SubToken, host)
	}
	return v
}

// InboundIDs returns inbound ids for the given users.
func (s *Service) InboundIDs(userIDs []int64) map[int64][]int64 {
	out := map[int64][]int64{}
	if len(userIDs) == 0 {
		return out
	}
	var links []db.UserInbound
	s.DB.Where("user_id IN ?", userIDs).Order("inbound_id").Find(&links)
	for _, l := range links {
		out[l.UserID] = append(out[l.UserID], l.InboundID)
	}
	return out
}

// ListParams filters the user list.
type ListParams struct {
	Search  string
	Status  string
	Page    int
	PerPage int
	Sort    string
	// GroupID filters by group (0 = any, -1 = no group).
	GroupID int64
	// AdminID limits the list to users owned by this admin (nil = all).
	AdminID *int64
}

var sortCols = map[string]string{
	"created_at":     "created_at",
	"username":       "username",
	"used_traffic":   "(up + down)",
	"data_limit":     "data_limit",
	"expire_at":      "expire_at",
	"id":             "id",
	"last_online_at": "last_online_at",
}

// List returns a page of users.
func (s *Service) List(p ListParams, host string) ([]View, int64, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 {
		p.PerPage = 20
	}
	if p.PerPage > 500 {
		p.PerPage = 500
	}
	q := s.DB.Model(&db.User{})
	if p.Search != "" {
		like := "%" + strings.ToLower(p.Search) + "%"
		q = q.Where("LOWER(username) LIKE ? OR LOWER(note) LIKE ?", like, like)
	}
	if p.Status != "" {
		if p.Status == "online" {
			q = q.Where("id IN ?", append([]int64{0}, presence.Default.OnlineIDs(s.now())...))
		} else {
			q = q.Where("status = ?", p.Status)
		}
	}
	switch {
	case p.GroupID > 0:
		q = q.Where("group_id = ?", p.GroupID)
	case p.GroupID == -1:
		q = q.Where("group_id IS NULL")
	}
	if p.AdminID != nil {
		q = q.Where("admin_id = ?", *p.AdminID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sort := p.Sort
	if sort == "" {
		sort = "-created_at"
	}
	desc := strings.HasPrefix(sort, "-")
	col, ok := sortCols[strings.TrimPrefix(sort, "-")]
	if !ok {
		col, desc = "created_at", true
	}
	order := col
	if desc {
		order += " DESC"
	}
	order += ", id DESC"
	var rows []db.User
	if err := q.Order(order).Offset((p.Page - 1) * p.PerPage).Limit(p.PerPage).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	links := s.InboundIDs(ids)
	out := make([]View, 0, len(rows))
	for i := range rows {
		out = append(out, s.ToView(&rows[i], links[rows[i].ID], host))
	}
	return out, total, nil
}

// GetModel loads a user row.
func (s *Service) GetModel(id int64) (*db.User, error) {
	var u db.User
	if err := s.DB.First(&u, id).Error; err != nil {
		if db.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// Get returns a user view.
func (s *Service) Get(id int64, host string) (*View, error) {
	u, err := s.GetModel(id)
	if err != nil {
		return nil, err
	}
	v := s.ToView(u, s.InboundIDs([]int64{id})[id], host)
	return &v, nil
}

func (s *Service) validateInbounds(tx *gorm.DB, ids []int64) ([]int64, error) {
	seen := map[int64]bool{}
	var uniq []int64
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	if len(uniq) == 0 {
		return uniq, nil
	}
	var n int64
	tx.Model(&db.Inbound{}).Where("id IN ?", uniq).Count(&n)
	if int(n) != len(uniq) {
		return nil, verr("one or more inbound_ids do not exist")
	}
	return uniq, nil
}

func setLinks(tx *gorm.DB, userID int64, ids []int64) error {
	if err := tx.Where("user_id = ?", userID).Delete(&db.UserInbound{}).Error; err != nil {
		return err
	}
	for _, iid := range ids {
		if err := tx.Create(&db.UserInbound{UserID: userID, InboundID: iid}).Error; err != nil {
			return err
		}
	}
	return nil
}

func applyInput(u *db.User, in *Input, creating bool) error {
	if in.Username.Set {
		name := strings.TrimSpace(in.Username.Val)
		if !usernameRe.MatchString(name) {
			return verr("username must be 3-32 chars: letters, digits, _ . @ -")
		}
		u.Username = name
	} else if creating {
		return verr("username is required")
	}
	if in.DataLimit.Set {
		if in.DataLimit.Val < 0 {
			return verr("data_limit must be >= 0")
		}
		u.DataLimit = in.DataLimit.Val
	}
	if in.ExpireAt.Set {
		if in.ExpireAt.Null {
			u.ExpireAt = nil
		} else {
			t := in.ExpireAt.Val.UTC()
			u.ExpireAt = &t
		}
	}
	if in.OnHoldDays.Set {
		if in.OnHoldDays.Null || in.OnHoldDays.Val == 0 {
			u.OnHoldDays = nil
		} else {
			if in.OnHoldDays.Val < 0 || in.OnHoldDays.Val > 3650 {
				return verr("on_hold_days must be 1-3650")
			}
			d := in.OnHoldDays.Val
			u.OnHoldDays = &d
		}
	}
	if in.ResetStrategy.Set {
		switch in.ResetStrategy.Val {
		case db.ResetNone, db.ResetDay, db.ResetWeek, db.ResetMonth:
			u.ResetStrategy = in.ResetStrategy.Val
		case "":
			u.ResetStrategy = db.ResetNone
		default:
			return verr("reset_strategy must be no_reset, day, week or month")
		}
	}
	if u.ResetStrategy == "" {
		u.ResetStrategy = db.ResetNone
	}
	if in.Note.Set {
		if len(in.Note.Val) > 2000 {
			return verr("note too long")
		}
		u.Note = in.Note.Val
	}
	if err := applyLimits(u, in); err != nil {
		return err
	}
	if in.Status.Set && !in.Status.Null {
		switch in.Status.Val {
		case db.StatusActive, db.StatusDisabled:
		default:
			return verr("status can only be set to active or disabled")
		}
	}
	return nil
}

// decideStatus sets the status after edits. requested is "" (keep), active or disabled.
func decideStatus(u *db.User, requested string, now time.Time) {
	if requested == db.StatusDisabled {
		u.Status = db.StatusDisabled
		return
	}
	if requested != db.StatusActive && u.Status == db.StatusDisabled {
		return
	}
	if u.OnHoldDays != nil && u.ExpireAt != nil {
		// An explicit expiry wins over on-hold.
		u.OnHoldDays = nil
	}
	if u.OnHoldDays != nil && u.ExpireAt == nil {
		if u.Status != db.StatusOnHold {
			// Entering on-hold: the period starts at the next connection.
			u.LastOnlineAt = nil
		}
		u.Status = db.StatusOnHold
		Evaluate(u, now)
		return
	}
	u.Status = db.StatusActive
	Evaluate(u, now)
}

// Create adds a new user.
func (s *Service) Create(in *Input, host string) (*View, error) {
	return s.CreateOwned(in, host, nil)
}

// CreateOwned adds a new user owned by a sub-admin (owner nil = sudo).
// Group defaults fill the fields the input leaves out.
func (s *Service) CreateOwned(in *Input, host string, owner *int64) (*View, error) {
	now := s.now()
	u := &db.User{
		UUID: util.UUIDv4(), Password: util.RandString(24), SubToken: util.RandString(32),
		ResetStrategy: db.ResetNone, LastResetAt: &now, TrafficMultiplier: 1, AdminID: owner,
	}
	if in.GroupID.Set && !in.GroupID.Null {
		if err := s.applyGroupDefaults(in, now); err != nil {
			return nil, err
		}
	}
	if err := applyInput(u, in, true); err != nil {
		return nil, err
	}
	req := ""
	if in.Status.Set {
		req = in.Status.Val
	}
	decideStatus(u, req, now)
	s.suspend(u, now, s.loc())
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var n int64
		tx.Model(&db.User{}).Where("username = ?", u.Username).Count(&n)
		if n > 0 {
			return verr("username already exists")
		}
		if err := validateGroup(tx, u.GroupID); err != nil {
			return err
		}
		var ids []int64
		if in.InboundIDs.Set {
			var err error
			if ids, err = s.validateInbounds(tx, in.InboundIDs.Val); err != nil {
				return err
			}
		}
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		return setLinks(tx, u.ID, ids)
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	return s.Get(u.ID, host)
}

// Update patches a user.
func (s *Service) Update(id int64, in *Input, host string) (*View, error) {
	loc := s.loc()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var u db.User
		if err := tx.First(&u, id).Error; err != nil {
			if db.IsNotFound(err) {
				return ErrNotFound
			}
			return err
		}
		oldName := u.Username
		if err := applyInput(&u, in, false); err != nil {
			return err
		}
		if in.GroupID.Set {
			if err := validateGroup(tx, u.GroupID); err != nil {
				return err
			}
		}
		if u.Username != oldName {
			var n int64
			tx.Model(&db.User{}).Where("username = ? AND id <> ?", u.Username, id).Count(&n)
			if n > 0 {
				return verr("username already exists")
			}
		}
		req := ""
		if in.Status.Set {
			req = in.Status.Val
		}
		decideStatus(&u, req, s.now())
		s.suspend(&u, s.now(), loc)
		if err := tx.Omit("up", "down", "daily_used").Save(&u).Error; err != nil {
			return err
		}
		if in.InboundIDs.Set {
			ids, err := s.validateInbounds(tx, in.InboundIDs.Val)
			if err != nil {
				return err
			}
			return setLinks(tx, id, ids)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	return s.Get(id, host)
}

// Delete removes users by id.
func (s *Service) Delete(ids ...int64) (int64, error) {
	var n int64
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id IN ?", ids).Delete(&db.User{})
		if res.Error != nil {
			return res.Error
		}
		n = res.RowsAffected
		if err := tx.Where("user_id IN ?", ids).Delete(&db.WGPeer{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id IN ?", ids).Delete(&db.UserIP{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id IN ?", ids).Delete(&db.UserDevice{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id IN ?", ids).Delete(&db.UserInbound{}).Error
	})
	if err == nil && n > 0 {
		s.changed()
	}
	return n, err
}

// ResetTraffic zeroes usage and re-evaluates status.
func (s *Service) ResetTraffic(ids ...int64) error {
	now := s.now()
	loc := s.loc()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var rows []db.User
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			u := &rows[i]
			u.Up, u.Down, u.DailyUsed = 0, 0, 0
			u.LastResetAt, u.DailyResetAt = &now, &now
			Evaluate(u, now)
			s.suspend(u, now, loc)
			if err := tx.Save(u).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.changed()
	}
	return err
}

// RevokeSub rotates the subscription token and credentials.
func (s *Service) RevokeSub(id int64, host string) (*View, error) {
	res := s.DB.Model(&db.User{}).Where("id = ?", id).Updates(map[string]any{
		"sub_token": util.RandString(32), "uuid": util.UUIDv4(), "password": util.RandString(24), "updated_at": s.now(),
	})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	s.changed()
	return s.Get(id, host)
}

// SetDisabled enables or disables users.
func (s *Service) SetDisabled(disabled bool, ids ...int64) error {
	now := s.now()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var rows []db.User
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			u := &rows[i]
			if disabled {
				u.Status = db.StatusDisabled
			} else {
				decideStatus(u, db.StatusActive, now)
			}
			if err := tx.Omit("up", "down", "daily_used").Save(u).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.changed()
	}
	return err
}

// Extend adds days to users' expiry. Users without expiry are left as is;
// expired users are extended from now.
func (s *Service) Extend(days int, ids ...int64) error {
	if days < 1 || days > 3650 {
		return verr("days must be 1-3650")
	}
	now := s.now()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var rows []db.User
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			u := &rows[i]
			switch {
			case u.Status == db.StatusOnHold && u.OnHoldDays != nil:
				d := *u.OnHoldDays + days
				u.OnHoldDays = &d
			case u.ExpireAt != nil:
				base := *u.ExpireAt
				if base.Before(now) {
					base = now
				}
				t := base.AddDate(0, 0, days)
				u.ExpireAt = &t
				Evaluate(u, now)
			default:
				continue
			}
			if err := tx.Omit("up", "down", "daily_used").Save(u).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.changed()
	}
	return err
}
