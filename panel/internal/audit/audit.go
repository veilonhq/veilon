// Package audit records administrative actions.
package audit

import (
	"log/slog"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Log stores an audit entry; failures are logged, never returned.
func Log(gdb *gorm.DB, admin *db.Admin, action, target, ip string) {
	e := db.AuditLog{Action: action, Target: target, IP: ip}
	if admin != nil {
		e.AdminID = admin.ID
		e.AdminName = admin.Username
	}
	if err := gdb.Create(&e).Error; err != nil {
		slog.Warn("audit log write failed", "err", err)
	}
}

// Entry is the API view of an audit row.
type Entry struct {
	ID        int64  `json:"id"`
	Admin     string `json:"admin"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

// List returns a page of entries, newest first.
func List(gdb *gorm.DB, page, perPage int) ([]Entry, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	var total int64
	if err := gdb.Model(&db.AuditLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []db.AuditLog
	if err := gdb.Order("id DESC").Offset((page - 1) * perPage).Limit(perPage).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{ID: r.ID, Admin: r.AdminName, Action: r.Action, Target: r.Target, IP: r.IP,
			CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")})
	}
	return out, total, nil
}
