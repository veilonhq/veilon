package users

import (
	"net"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// IPView is one entry of a user's IP log.
type IPView struct {
	ID        int64     `json:"id"`
	IP        string    `json:"ip"`
	Country   string    `json:"country"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int64     `json:"count"`
	Blocked   bool      `json:"blocked"`
}

// DeviceView is one entry of a user's device log.
type DeviceView struct {
	ID        int64     `json:"id"`
	HWID      string    `json:"hwid"`
	OS        string    `json:"os"`
	OSVersion string    `json:"os_version"`
	Model     string    `json:"model"`
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
	Country   string    `json:"country"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int64     `json:"count"`
	Blocked   bool      `json:"blocked"`
}

// ConnLog is a user's connection log.
type ConnLog struct {
	IPs     []IPView     `json:"ips"`
	Devices []DeviceView `json:"devices"`
}

// Connections returns the user's IP and device log, most recent first.
func (s *Service) Connections(userID int64) (*ConnLog, error) {
	if _, err := s.GetModel(userID); err != nil {
		return nil, err
	}
	var ips []db.UserIP
	if err := s.DB.Where("user_id = ?", userID).Order("last_seen DESC").Limit(500).Find(&ips).Error; err != nil {
		return nil, err
	}
	var devs []db.UserDevice
	if err := s.DB.Where("user_id = ?", userID).Order("last_seen DESC").Limit(200).Find(&devs).Error; err != nil {
		return nil, err
	}
	out := &ConnLog{IPs: []IPView{}, Devices: []DeviceView{}}
	cc := map[string]string{}
	for _, r := range ips {
		cc[r.IP] = r.Country
		out.IPs = append(out.IPs, IPView{ID: r.ID, IP: r.IP, Country: r.Country, FirstSeen: r.FirstSeen, LastSeen: r.LastSeen, Count: r.Hits, Blocked: r.Blocked})
	}
	for _, d := range devs {
		out.Devices = append(out.Devices, DeviceView{ID: d.ID, HWID: d.HWID, OS: d.OS, OSVersion: d.OSVersion, Model: d.Model,
			UserAgent: d.UserAgent, IP: d.IP, Country: cc[d.IP], FirstSeen: d.FirstSeen, LastSeen: d.LastSeen, Count: d.Hits, Blocked: d.Blocked})
	}
	return out, nil
}

// BlockIP blocks or unblocks a client IP for a user. The IP is added to the
// log when it is not there yet (blocking an address before it connects).
func (s *Service) BlockIP(userID int64, ip string, blocked bool) error {
	p := net.ParseIP(strings.TrimSpace(ip))
	if p == nil {
		return verr("invalid IP address")
	}
	if v4 := p.To4(); v4 != nil {
		p = v4
	}
	if _, err := s.GetModel(userID); err != nil {
		return err
	}
	now := s.now()
	row := db.UserIP{UserID: userID, IP: p.String(), FirstSeen: now, LastSeen: now, Blocked: blocked}
	err := s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ip"}},
		DoUpdates: clause.Assignments(map[string]any{"blocked": blocked}),
	}).Create(&row).Error
	if err == nil {
		s.changed()
	}
	return err
}

// DeleteIP removes an IP from the user's log (and unblocks it).
func (s *Service) DeleteIP(userID, id int64) error {
	res := s.DB.Where("user_id = ? AND id = ?", userID, id).Delete(&db.UserIP{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.changed()
	return nil
}

// BlockDevice blocks or unblocks a device: a blocked device gets an empty
// subscription.
func (s *Service) BlockDevice(userID, id int64, blocked bool) error {
	res := s.DB.Model(&db.UserDevice{}).Where("user_id = ? AND id = ?", userID, id).Update("blocked", blocked)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDevice forgets a device, freeing a slot of the device limit.
func (s *Service) DeleteDevice(userID, id int64) error {
	res := s.DB.Where("user_id = ? AND id = ?", userID, id).Delete(&db.UserDevice{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Device is what a subscription request says about the client.
type Device struct {
	HWID, OS, OSVersion, Model, UserAgent, IP string
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// DeviceDecision is the result of CheckDevice.
type DeviceDecision struct {
	Allowed bool
	// Reason is set when refused: "hwid_required", "device_blocked" or
	// "device_limit".
	Reason string
}

// CheckDevice records the device of a subscription request and decides
// whether it may receive configs:
//   - a blocked device is refused;
//   - without an HWID the request is refused when the user requires one,
//     and allowed (not recorded) otherwise;
//   - a new device beyond the device limit is refused (and not recorded),
//     so earlier devices keep working; delete a device to free its slot.
func CheckDevice(gdb *gorm.DB, u *db.User, d Device, now time.Time) DeviceDecision {
	d.HWID = clip(d.HWID, 128)
	if d.HWID == "" {
		if u.RequireHWID {
			return DeviceDecision{Reason: "hwid_required"}
		}
		return DeviceDecision{Allowed: true}
	}
	var dev db.UserDevice
	err := gdb.Where("user_id = ? AND hwid = ?", u.ID, d.HWID).Limit(1).Find(&dev).Error
	if err != nil {
		return DeviceDecision{Allowed: true} // never lock users out on a DB hiccup
	}
	if dev.ID != 0 {
		if dev.Blocked {
			return DeviceDecision{Reason: "device_blocked"}
		}
		gdb.Model(&db.UserDevice{}).Where("id = ?", dev.ID).Updates(map[string]any{
			"last_seen": now, "hits": gorm.Expr("hits + 1"), "ip": clip(d.IP, 64),
			"os": clip(d.OS, 64), "os_version": clip(d.OSVersion, 64), "model": clip(d.Model, 128), "user_agent": clip(d.UserAgent, 255),
		})
		return DeviceDecision{Allowed: true}
	}
	if u.DeviceLimit > 0 {
		var n int64
		gdb.Model(&db.UserDevice{}).Where("user_id = ? AND blocked = ?", u.ID, false).Count(&n)
		if int(n) >= u.DeviceLimit {
			return DeviceDecision{Reason: "device_limit"}
		}
	}
	gdb.Clauses(clause.OnConflict{DoNothing: true}).Create(&db.UserDevice{
		UserID: u.ID, HWID: d.HWID, OS: clip(d.OS, 64), OSVersion: clip(d.OSVersion, 64), Model: clip(d.Model, 128),
		UserAgent: clip(d.UserAgent, 255), IP: clip(d.IP, 64), FirstSeen: now, LastSeen: now, Hits: 1,
	})
	return DeviceDecision{Allowed: true}
}
