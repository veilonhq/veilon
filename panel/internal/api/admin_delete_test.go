package api

import (
	"fmt"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// The API refuses the same deletes as `veilon admin delete`: an admin who
// still owns users, and the last full (sudo) admin.
func TestAPIAdminDeleteRules(t *testing.T) {
	c, srv := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)

	code, m, _ := c.do("POST", api+"/admins", map[string]any{"username": "helper", "password": "password123", "is_sudo": false})
	if code != 201 {
		t.Fatalf("create admin %d %v", code, m)
	}
	hid := int64(m["id"].(float64))
	u := db.User{Username: "owned", SubToken: "tok-owned", Status: "active", ResetStrategy: "no_reset", AdminID: &hid}
	if err := srv.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	code, m, _ = c.do("DELETE", fmt.Sprintf("%s/admins/%d", api, hid), nil)
	if code != 409 || m["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("delete admin who owns users: %d %v", code, m)
	}
	var n int64
	srv.DB.Model(&db.Admin{}).Where("id = ?", hid).Count(&n)
	if n != 1 {
		t.Fatal("admin was deleted although it owns users")
	}

	// Once the user is gone the delete goes through.
	srv.DB.Delete(&db.User{}, u.ID)
	if code, m, _ = c.do("DELETE", fmt.Sprintf("%s/admins/%d", api, hid), nil); code != 200 {
		t.Fatalf("delete admin: %d %v", code, m)
	}

	// A second sudo admin can be deleted; the last one cannot (not even by
	// another route than self-delete).
	code, m, _ = c.do("POST", api+"/admins", map[string]any{"username": "boss", "password": "password123", "is_sudo": true})
	if code != 201 {
		t.Fatalf("create sudo %d %v", code, m)
	}
	bid := int64(m["id"].(float64))
	if code, m, _ = c.do("DELETE", fmt.Sprintf("%s/admins/%d", api, bid), nil); code != 200 {
		t.Fatalf("delete second sudo: %d %v", code, m)
	}
	if code, m, _ = c.do("DELETE", api+"/admins/999", nil); code != 404 {
		t.Fatalf("delete missing admin: %d %v", code, m)
	}
}
