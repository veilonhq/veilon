package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// xrayTemplate returns the admin's Xray template (outbounds, routing,
// balancers, DNS and basic options) plus the inbound tags rules may use.
func (s *Server) xrayTemplate(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	var ibs []db.Inbound
	s.DB.Select("id", "remark", "protocol").Order("id").Find(&ibs)
	tags := make([]map[string]any, 0, len(ibs))
	for _, ib := range ibs {
		tags = append(tags, map[string]any{"id": ib.ID, "tag": "inbound-" + strconv.FormatInt(ib.ID, 10), "remark": ib.Remark, "protocol": ib.Protocol})
	}
	writeJSON(w, 200, map[string]any{"template": c.LoadTemplate(), "custom": db.GetSetting(s.DB, xray.KeyTemplate) != "",
		"inbounds": tags, "reserved": []string{xray.TagDirect, xray.TagBlocked}})
}

// readTemplate decodes {"template": {...}} and validates it.
func readTemplate(w http.ResponseWriter, r *http.Request) (*xray.Template, bool) {
	var body struct {
		Template json.RawMessage `json:"template"`
	}
	if !decode(w, r, &body) {
		return nil, false
	}
	if len(body.Template) == 0 || string(body.Template) == "null" {
		writeErr(w, http.StatusBadRequest, "validation_error", "template is required")
		return nil, false
	}
	t, err := xray.ParseTemplate(body.Template)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return nil, false
	}
	return t, true
}

func (s *Server) xrayTemplateTest(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	t, ok := readTemplate(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, c.TestTemplate(r.Context(), t))
}

func (s *Server) xrayTemplateApply(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	t, ok := readTemplate(w, r)
	if !ok {
		return
	}
	res, err := c.ApplyTemplate(r.Context(), t)
	if err != nil {
		if errors.Is(err, xray.ErrBusy) {
			writeErr(w, http.StatusConflict, "busy", err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "core_error", err.Error())
		return
	}
	if res.Applied {
		audit.Log(s.DB, a, "xray.config", "template", s.clientIP(r))
	}
	writeJSON(w, 200, map[string]any{"result": res, "template": c.LoadTemplate()})
}

// xrayParseLink turns a share link into an outbound object.
func (s *Server) xrayParseLink(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var body struct {
		Link string `json:"link"`
		Tag  string `json:"tag"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := xray.ParseShareLink(body.Link, body.Tag)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"outbound": out})
}
