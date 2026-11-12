package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func (r *rig) root() *db.Admin {
	var root db.Admin
	r.gdb.Where("username = ?", "root").First(&root)
	return &root
}

// createNode adds a node that is never reachable (closed local port).
func (r *rig) createNode(name string) int64 {
	r.t.Helper()
	st, raw := r.srv.Do(context.Background(), r.root(), "POST", "/nodes", map[string]any{"name": name, "address": "127.0.0.1", "api_port": 1})
	if st != 201 {
		r.t.Fatalf("node %d %s", st, raw)
	}
	var res struct {
		Node struct {
			ID int64 `json:"id"`
		} `json:"node"`
	}
	_ = json.Unmarshal(raw, &res)
	return res.Node.ID
}

func (r *rig) nodeExists(id int64) bool {
	var n int64
	r.gdb.Model(&db.Node{}).Where("id = ?", id).Count(&n)
	return n == 1
}

func TestNodesListAndCreate(t *testing.T) {
	r := newRig(t)
	r.createNode("Frankfurt")
	r.start()
	list := r.press("nl", "<b>نود ها</b>")
	// a node that was never reached is shown as waiting
	if !strings.Contains(fmt.Sprint(list.p["reply_markup"]), "⏳ Frankfurt") {
		t.Fatalf("list %v", list.p["reply_markup"])
	}
	r.press(button(t, list, "نود جدید"), "یک نام برای نود")
	r.typeText("Paris", "آدرس IP")
	c := r.typeText("127.0.0.1", "پورت ارتباط")
	c = r.press(button(t, c, "پیش فرض"), "دستور نصب نود Paris")
	text := fmt.Sprint(c.p["text"])
	var shown bool
	for _, m := range r.tg.since(0) {
		if m.method == "sendDocument" && strings.Contains(m.p["_data"].(string), "node --token vn1.") {
			shown = true
		}
	}
	if !strings.Contains(text, "node --token vn1.") && !shown {
		t.Fatalf("no install command: %s", text)
	}
	var n db.Node
	r.gdb.Where("name = ?", "Paris").Limit(1).Find(&n)
	if n.ID == 0 || n.APIPort != 62050 {
		t.Fatalf("node %+v", n)
	}
}

func TestNodeDeleteNeedsConfirm(t *testing.T) {
	r := newRig(t)
	id := r.createNode("Berlin")
	r.start()
	v := r.press(fmt.Sprintf("nv:%d", id), "Berlin")
	c := r.press(button(t, v, "حذف"), "حذف شود")
	if !r.nodeExists(id) {
		t.Fatal("deleted before confirm")
	}
	r.press(button(t, c, "نه"), "Berlin")
	if !r.nodeExists(id) {
		t.Fatal("deleted on no")
	}
	c = r.press(fmt.Sprintf("nd:%d", id), "حذف شود")
	r.press(button(t, c, "بله"), "<b>نود ها</b>")
	if r.nodeExists(id) {
		t.Fatal("node still exists")
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "node.delete").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
}

// A node whose only inbound would land on a port the main server already
// uses is not deleted (the panel's own port check), and the bot says why.
func TestNodeDeletePortCheck(t *testing.T) {
	r := newRig(t)
	id := r.createNode("Rome")
	st, raw := r.srv.Do(context.Background(), r.root(), "POST", "/inbounds", map[string]any{"remark": "NodeOnly", "protocol": "vless", "port": 4443,
		"security": "reality", "flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}, "node_ids": []int64{id}})
	if st != 201 {
		t.Fatalf("inbound %d %s", st, raw)
	}
	r.start()
	c := r.press(fmt.Sprintf("nd:%d", id), "حذف شود")
	c = r.press(button(t, c, "بله"), "انجام نشد")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "4443") {
		t.Fatalf("error text %v", c.p["text"])
	}
	if !r.nodeExists(id) {
		t.Fatal("node deleted despite the port conflict")
	}
}

func TestNodeToggle(t *testing.T) {
	r := newRig(t)
	id := r.createNode("Oslo")
	r.start()
	v := r.press(fmt.Sprintf("nv:%d", id), "Oslo")
	r.press(button(t, v, "خاموش کردن"), "⛔️ خاموش")
	var n db.Node
	r.gdb.First(&n, id)
	if n.Enabled {
		t.Fatal("node still enabled")
	}
}
