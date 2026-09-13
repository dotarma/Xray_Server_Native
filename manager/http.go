package main

import (
	"net/http"
)

// handler owns the manager's HTTP surface. Keeping registration and middleware
// composition here prevents lifecycle and deployment code from depending on
// transport details.
func (m *manager) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", m.handleHealth)
	mux.HandleFunc("/api/status", m.handleStatus)
	mux.HandleFunc("/api/services", m.handleServices)
	mux.HandleFunc("/api/tunnel/token", m.handleTunnelToken)
	mux.HandleFunc("/api/deploy/quick", m.handleQuickDeploy)
	mux.HandleFunc("/api/deploy/mode2", m.handleModeTwoDeploy)
	mux.HandleFunc("/api/deploy/mode3", m.handleModeThreeDeploy)
	mux.HandleFunc("/api/logs", m.handleLogs)
	mux.HandleFunc("/", m.handleStatic)

	return requestLogger(securityHeaders(m.requirePanelAuth(m.requireSameOrigin(mux))))
}

func (m *manager) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"build":          managerBuild,
		"stateSchema":    currentStateSchemaVersion,
		"stateDirectory": m.stateDir != "",
	})
}
