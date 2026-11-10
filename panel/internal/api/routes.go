package api

import (
	"net/http"

	"github.com/veilonhq/veilon/panel/internal/auth"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/version"
)

func (s *Server) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "version": version.Version, "schema": db.SchemaVersion(s.DB)})
	})

	m.HandleFunc("POST /auth/login", s.login)
	m.HandleFunc("GET /auth/me", s.authn(s.me))
	m.HandleFunc("PATCH /auth/me", s.authn(s.updateMe))
	m.HandleFunc("POST /auth/password", s.authn(s.changePassword))

	m.HandleFunc("GET /system/stats", s.authn(s.systemStats))
	m.HandleFunc("GET /system/traffic", s.authn(s.systemTraffic))
	m.HandleFunc("POST /core/restart", s.perm(auth.PermSettings, s.coreRestart))
	m.HandleFunc("GET /core/xray", s.perm(auth.PermSettings, s.coreXray))
	m.HandleFunc("POST /core/start", s.perm(auth.PermSettings, s.corePower(true)))
	m.HandleFunc("POST /core/stop", s.perm(auth.PermSettings, s.corePower(false)))
	m.HandleFunc("GET /core/xray/releases", s.perm(auth.PermSettings, s.coreReleases))
	m.HandleFunc("POST /core/xray/install", s.perm(auth.PermSettings, s.coreInstall))
	m.HandleFunc("POST /core/xray/geo", s.perm(auth.PermSettings, s.coreGeoUpdate))
	m.HandleFunc("GET /xray/template", s.sudo(s.xrayTemplate))
	m.HandleFunc("POST /xray/template/test", s.sudo(s.xrayTemplateTest))
	m.HandleFunc("PUT /xray/template", s.sudo(s.xrayTemplateApply))
	m.HandleFunc("POST /xray/outbounds/parse", s.sudo(s.xrayParseLink))
	m.HandleFunc("GET /core/logs", s.perm(auth.PermLogs, s.coreLogs))
	m.HandleFunc("GET /core/config", s.perm(auth.PermLogs, s.coreConfig))

	m.HandleFunc("GET /users", s.perm(auth.PermUsersView, s.listUsers))
	m.HandleFunc("POST /users", s.perm(auth.PermUsersCreate, s.createUser))
	m.HandleFunc("POST /users/bulk", s.authn(s.bulkUsers))
	m.HandleFunc("GET /users/{id}", s.userPerm(auth.PermUsersView, s.getUser))
	m.HandleFunc("PATCH /users/{id}", s.userPerm(auth.PermUsersEdit, s.updateUser))
	m.HandleFunc("DELETE /users/{id}", s.userPerm(auth.PermUsersDelete, s.deleteUser))
	m.HandleFunc("POST /users/{id}/reset-traffic", s.userPerm(auth.PermUsersEdit, s.resetUserTraffic))
	m.HandleFunc("POST /users/{id}/revoke-sub", s.userPerm(auth.PermUsersEdit, s.revokeUserSub))
	m.HandleFunc("GET /users/{id}/links", s.userPerm(auth.PermUsersView, s.userLinks))
	m.HandleFunc("GET /users/{id}/connections", s.userPerm(auth.PermUsersView, s.userConnections))
	m.HandleFunc("POST /users/{id}/ips", s.userPerm(auth.PermUsersEdit, s.userBlockIP))
	m.HandleFunc("DELETE /users/{id}/ips/{sub}", s.userPerm(auth.PermUsersEdit, s.userDeleteIP))
	m.HandleFunc("PATCH /users/{id}/devices/{sub}", s.userPerm(auth.PermUsersEdit, s.userBlockDevice))
	m.HandleFunc("DELETE /users/{id}/devices/{sub}", s.userPerm(auth.PermUsersEdit, s.userDeleteDevice))

	m.HandleFunc("GET /groups", s.anyPerm([]string{auth.PermGroups, auth.PermUsersView}, s.listGroups))
	m.HandleFunc("POST /groups", s.perm(auth.PermGroups, s.createGroup))
	m.HandleFunc("PATCH /groups/{id}", s.perm(auth.PermGroups, s.updateGroup))
	m.HandleFunc("DELETE /groups/{id}", s.perm(auth.PermGroups, s.deleteGroup))

	m.HandleFunc("GET /inbounds", s.anyPerm([]string{auth.PermInbounds, auth.PermUsersView, auth.PermUsersCreate, auth.PermUsersEdit, auth.PermGroups}, s.listInbounds))
	m.HandleFunc("POST /inbounds", s.perm(auth.PermInbounds, s.createInbound))
	m.HandleFunc("POST /inbounds/reality-keys", s.perm(auth.PermInbounds, s.realityKeys))
	m.HandleFunc("POST /inbounds/wireguard-keys", s.perm(auth.PermInbounds, s.wireguardKeys))
	m.HandleFunc("GET /inbounds/{id}", s.perm(auth.PermInbounds, s.getInbound))
	m.HandleFunc("PATCH /inbounds/{id}", s.perm(auth.PermInbounds, s.updateInbound))
	m.HandleFunc("DELETE /inbounds/{id}", s.perm(auth.PermInbounds, s.deleteInbound))

	m.HandleFunc("GET /certs", s.perm(auth.PermCerts, s.listCerts))
	m.HandleFunc("POST /certs/acme", s.perm(auth.PermCerts, s.acmeCert))
	m.HandleFunc("POST /certs/upload", s.perm(auth.PermCerts, s.uploadCert))
	m.HandleFunc("POST /certs/self-signed", s.perm(auth.PermCerts, s.selfSignedCert))
	m.HandleFunc("DELETE /certs/{id}", s.perm(auth.PermCerts, s.deleteCert))
	m.HandleFunc("GET /certs/public-ip", s.perm(auth.PermCerts, s.publicIP))
	m.HandleFunc("GET /certs/{id}", s.perm(auth.PermCerts, s.getCert))
	m.HandleFunc("PATCH /certs/{id}", s.perm(auth.PermCerts, s.patchCert))
	m.HandleFunc("POST /certs/{id}/renew", s.perm(auth.PermCerts, s.renewCert))
	m.HandleFunc("POST /certs/{id}/revoke", s.perm(auth.PermCerts, s.revokeCert))

	m.HandleFunc("GET /settings", s.authn(s.getSettings))
	m.HandleFunc("PATCH /settings", s.perm(auth.PermSettings, s.patchSettings))

	m.HandleFunc("GET /admins", s.sudo(s.listAdmins))
	m.HandleFunc("POST /admins", s.sudo(s.createAdmin))
	m.HandleFunc("PATCH /admins/{id}", s.sudo(s.updateAdmin))
	m.HandleFunc("DELETE /admins/{id}", s.sudo(s.deleteAdmin))

	m.HandleFunc("GET /audit", s.perm(auth.PermLogs, s.listAudit))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := m.Handler(r); pattern == "" {
			writeErr(w, http.StatusNotFound, "not_found", "no such endpoint or method")
			return
		}
		m.ServeHTTP(w, r)
	})
}
