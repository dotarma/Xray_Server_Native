package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	managerTag                 = "android-mini-server-vless-ws"
	modeOrigin                 = "http://127.0.0.1:8888"
	modeServerPort             = 8888
	modeWSInternalPort         = 28888
	modeXHTTPInternalPort      = 38888
	modeThreeWSInternalPort    = 28080
	modeThreeXHTTPInternalPort = 38080
	managerBuild               = "0.6.20"
	cloudflareAPI              = "https://api.cloudflare.com/client/v4"
	maxRequestBytes            = 32 << 10
	panelPortDefault           = 2053
	modeThreeOriginPort        = 8080
	fakeTikTokSNI              = "api24-normal-alisg.tiktokv.com"
	fakeTikTokLabel            = "Free Tiktok"
	fakeVinaSNI                = "vnpt.theworkpc.com"
	fakeVinaLabel              = "Free Vina Ko Nen"
)

var (
	domainPattern = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	pathPattern   = regexp.MustCompile(`^/[A-Za-z0-9._~-]{1,96}$`)
	idPattern     = regexp.MustCompile(`^[a-fA-F0-9-]{36}$`)
)

type manager struct {
	moduleDir string
	stateDir  string
	webRoot   string
	mu        sync.Mutex
}

type serviceStatus struct {
	Running bool   `json:"running"`
	PID     string `json:"pid,omitempty"`
}

type deployment struct {
	Mode             string   `json:"mode"`
	Host             string   `json:"host"`
	Path             string   `json:"path"`
	UUID             string   `json:"uuid"`
	Label            string   `json:"label"`
	CreatedAt        string   `json:"createdAt"`
	Link             string   `json:"link"`
	Links            []string `json:"links,omitempty"`
	FakeSNI          string   `json:"fakeSni,omitempty"`
	PortMode         string   `json:"portMode,omitempty"`
	Transport        string   `json:"transport,omitempty"`
	XHTTPMode        string   `json:"xhttpMode,omitempty"`
	CountryCode      string   `json:"countryCode,omitempty"`
	PanelURL         string   `json:"panelUrl,omitempty"`
	SubscriptionHost string   `json:"subscriptionHost,omitempty"`
	SubscriptionID   string   `json:"subscriptionId,omitempty"`
	SubscriptionURL  string   `json:"subscriptionUrl,omitempty"`
	OriginPort       int      `json:"originPort,omitempty"`
	ClientEmail      string   `json:"clientEmail,omitempty"`
}

type deploymentRegistry struct {
	Mode1 *deployment   `json:"mode1,omitempty"`
	Mode2 *deployment   `json:"mode2,omitempty"`
	Mode3 []*deployment `json:"mode3,omitempty"`
}

// savedConfigurations intentionally never contains connector tokens. Tokens
// remain in root-only files and an empty token field reuses the saved secret.
type savedConfigurations struct {
	Mode2 *nativeTunnelPreferences `json:"mode2,omitempty"`
	Mode3 *modeThreePreferences    `json:"mode3,omitempty"`
}

type nativeTunnelPreferences struct {
	Domain      string `json:"domain"`
	Label       string `json:"label"`
	FakeSNI     string `json:"fakeSni"`
	PortMode    string `json:"portMode"`
	Transport   string `json:"transport"`
	XHTTPMode   string `json:"xhttpMode"`
	CountryCode string `json:"countryCode"`
	TokenSaved  bool   `json:"tokenSaved"`
}

type modeThreePreferences struct {
	Domain             string `json:"domain"`
	SubscriptionDomain string `json:"subscriptionDomain"`
	PanelDomain        string `json:"panelDomain"`
	FakeSNI            string `json:"fakeSni"`
	PortMode           string `json:"portMode"`
	Transport          string `json:"transport"`
	XHTTPMode          string `json:"xhttpMode"`
	TokenSaved         bool   `json:"tokenSaved"`
}

type statusResponse struct {
	Panel            serviceStatus       `json:"panel"`
	PanelTunnel      serviceStatus       `json:"panelTunnel"`
	QuickServer      serviceStatus       `json:"quickServer"`
	NativeDemux      serviceStatus       `json:"nativeDemux"`
	PanelDemux       serviceStatus       `json:"panelDemux"`
	Tunnel           serviceStatus       `json:"tunnel"`
	Manager          serviceStatus       `json:"manager"`
	PanelPort        string              `json:"panelPort"`
	TunnelMode       string              `json:"tunnelMode"`
	TunnelConfigured bool                `json:"tunnelConfigured"`
	ActiveMode       string              `json:"activeMode"`
	QuickHostname    string              `json:"quickHostname,omitempty"`
	Deployment       *deployment         `json:"deployment,omitempty"`
	Deployments      deploymentRegistry  `json:"deployments"`
	Saved            savedConfigurations `json:"saved"`
	ManagerURL       string              `json:"managerUrl"`
	ManagerBuild     string              `json:"managerBuild"`
	PanelURL         string              `json:"panelUrl"`
	PanelUsername    string              `json:"panelUsername,omitempty"`
	PanelPassword    string              `json:"panelPassword,omitempty"`
	PanelTunnelURL   string              `json:"panelTunnelUrl,omitempty"`
	AndroidRelease   string              `json:"androidRelease"`
	DeviceABI        string              `json:"deviceAbi"`
}

type apiError struct {
	Error string `json:"error"`
}

type nativeTunnelInput struct {
	Domain      string `json:"domain"`
	Token       string `json:"token"`
	Label       string `json:"label"`
	FakeSNI     string `json:"fakeSni"`
	PortMode    string `json:"portMode"`
	Transport   string `json:"transport"`
	XHTTPMode   string `json:"xhttpMode"`
	CountryCode string `json:"countryCode"`
}

type panelTunnelInput struct {
	Domain             string `json:"domain"`
	SubscriptionDomain string `json:"subscriptionDomain"`
	PanelDomain        string `json:"panelDomain"`
	Token              string `json:"token"`
	FakeSNI            string `json:"fakeSni"`
	PortMode           string `json:"portMode"`
	Transport          string `json:"transport"`
	XHTTPMode          string `json:"xhttpMode"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:2036", "loopback listen address")
	demux := flag.Bool("demux", false, "run the native WS/xHTTP transport demultiplexer")
	demuxListen := flag.String("demux-listen", "127.0.0.1:8888", "transport demux listen address")
	demuxWS := flag.String("demux-ws", "127.0.0.1:28888", "transport demux WebSocket backend")
	demuxXHTTP := flag.String("demux-xhttp", "127.0.0.1:38888", "transport demux xHTTP backend")
	moduleDir := flag.String("module", "/data/adb/modules/android-mini-server-native", "module directory")
	stateDir := flag.String("state", "/data/adb/modules/android-mini-server-native", "state directory")
	webRoot := flag.String("web-root", "", "web asset directory")
	flag.Parse()
	if *demux {
		if err := runTransportDemux(*demuxListen, *demuxWS, *demuxXHTTP); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *webRoot == "" {
		*webRoot = filepath.Join(*moduleDir, "manager", "web")
	}
	m := &manager{moduleDir: *moduleDir, stateDir: *stateDir, webRoot: *webRoot}
	go m.reconcileQuickDeployment()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", m.handleStatus)
	mux.HandleFunc("/api/services", m.handleServices)
	mux.HandleFunc("/api/tunnel/token", m.handleTunnelToken)
	mux.HandleFunc("/api/deploy/quick", m.handleQuickDeploy)
	mux.HandleFunc("/api/deploy/mode2", m.handleModeTwoDeploy)
	mux.HandleFunc("/api/deploy/mode3", m.handleModeThreeDeploy)
	mux.HandleFunc("/api/logs", m.handleLogs)
	mux.HandleFunc("/", m.handleStatic)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       25 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runTransportDemux owns the public local origin for Mode 2 dual transport.
// It forwards the HTTP request unchanged after classifying its Upgrade header.
func runTransportDemux(listenAddress, wsAddress, xhttpAddress string) error {
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("transport demux cannot listen on %s: %w", listenAddress, err)
	}
	defer listener.Close()
	for {
		client, err := listener.Accept()
		if err != nil {
			return err
		}
		go proxyTransportConnection(client, wsAddress, xhttpAddress)
	}
}

func proxyTransportConnection(client net.Conn, wsAddress, xhttpAddress string) {
	defer client.Close()
	reader := bufio.NewReaderSize(client, 8192)
	client.SetReadDeadline(time.Now().Add(8 * time.Second))
	var header bytes.Buffer
	for header.Len() < 8192 {
		line, err := reader.ReadString('\n')
		header.WriteString(line)
		if strings.Contains(header.String(), "\r\n\r\n") || err != nil {
			break
		}
	}
	client.SetReadDeadline(time.Time{})
	if header.Len() == 0 {
		return
	}
	backendAddress := xhttpAddress
	if bytes.Contains(bytes.ToLower(header.Bytes()), []byte("upgrade: websocket")) {
		backendAddress = wsAddress
	}
	backend, err := net.DialTimeout("tcp", backendAddress, 8*time.Second)
	if err != nil {
		return
	}
	defer backend.Close()
	if _, err := backend.Write(header.Bytes()); err != nil {
		return
	}
	go func() { _, _ = io.Copy(backend, reader); backend.Close() }()
	_, _ = io.Copy(client, backend)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (m *manager) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" && r.URL.Path != "/app.css" && r.URL.Path != "/overrides.css" && r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	http.ServeFile(w, r, filepath.Join(m.webRoot, name))
}

func (m *manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	status, err := m.status()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (m *manager) handleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Service string `json:"service"`
		Action  string `json:"action"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	command, ok := serviceCommand(input.Service, input.Action)
	if !ok {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "Unsupported service action."})
		return
	}
	m.mu.Lock()
	output, err := m.control(command)
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: userSafeCommandError(err, output)})
		return
	}
	status, _ := m.status()
	writeJSON(w, http.StatusOK, map[string]any{"message": "Service command completed.", "status": status})
}

func (m *manager) handleTunnelToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	if !validSecret(input.Token, 32, 4096) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "Tunnel token is malformed."})
		return
	}
	m.mu.Lock()
	err := m.saveTunnelToken(strings.TrimSpace(input.Token), "named")
	if err == nil {
		_, err = m.control("restart-tunnel")
	}
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Tunnel token saved and connector restarted."})
}

func (m *manager) handleQuickDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input nativeTunnelInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	input.Label = normalizeLabel(input.Label)
	input.FakeSNI = normalizeAllowedFakeSNI(input.FakeSNI)
	input.PortMode = normalizePortMode(input.PortMode)
	input.CountryCode = normalizeCountryCode(input.CountryCode)
	if input.FakeSNI == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "Enter at least one Fake SNI hostname."})
		return
	}

	m.mu.Lock()
	deployed, err := m.deployQuick(input)
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, deployed)
}

// handleModeTwoDeploy mirrors the old script's Named Tunnel mode. The user
// creates the public hostname in Cloudflare Zero Trust, then supplies the
// matching hostname and connector token here. The native Xray listener stays
// independent from 3x-ui.
func (m *manager) handleModeTwoDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input nativeTunnelInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
	input.Token = strings.TrimSpace(input.Token)
	input.Label = normalizeLabel(input.Label)
	input.FakeSNI = normalizeAllowedFakeSNI(input.FakeSNI)
	input.PortMode = normalizePortMode(input.PortMode)
	input.Transport = normalizeNativeTransport(input.Transport)
	input.XHTTPMode = normalizeXHTTPMode(input.XHTTPMode)
	input.CountryCode = normalizeCountryCode(input.CountryCode)
	if input.Token == "" {
		input.Token = m.savedToken(filepath.Join(m.stateDir, "token.txt"))
	}
	if !domainPattern.MatchString(input.Domain) || !validSecret(input.Token, 32, 4096) || input.FakeSNI == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "Check the public hostname, connector token, and Fake SNI list."})
		return
	}

	m.mu.Lock()
	deployed, err := m.deployModeTwo(input)
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, deployed)
}

// handleModeThreeDeploy switches the primary 3x-ui tunnel from a Quick Tunnel
// to a user-created named tunnel. Cloudflare route management deliberately
// stays in Zero Trust, so this needs only the connector token.
func (m *manager) handleModeThreeDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input panelTunnelInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
	input.SubscriptionDomain = strings.ToLower(strings.TrimSpace(input.SubscriptionDomain))
	input.PanelDomain = strings.ToLower(strings.TrimSpace(input.PanelDomain))
	input.Token = strings.TrimSpace(input.Token)
	input.FakeSNI = normalizeAllowedFakeSNI(input.FakeSNI)
	input.PortMode = normalizePortMode(input.PortMode)
	input.Transport = normalizeNativeTransport(input.Transport)
	input.XHTTPMode = normalizeXHTTPMode(input.XHTTPMode)
	if input.Token == "" {
		input.Token = m.savedToken(filepath.Join(m.stateDir, "panel-tunnel-token.txt"))
	}
	if input.Domain == input.SubscriptionDomain || (input.PanelDomain != "" && (input.PanelDomain == input.Domain || input.PanelDomain == input.SubscriptionDomain)) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "VPN and subscription hostnames must be different Cloudflare routes."})
		return
	}
	if !domainPattern.MatchString(input.Domain) || !domainPattern.MatchString(input.SubscriptionDomain) || (input.PanelDomain != "" && !domainPattern.MatchString(input.PanelDomain)) || !validSecret(input.Token, 32, 4096) || input.FakeSNI == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "Enter valid VPN and subscription hostnames, a connector token, and a supported Fake SNI."})
		return
	}

	m.mu.Lock()
	deployed, err := m.deployModeThree(input)
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, deployed)
}

func (m *manager) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	target := r.URL.Query().Get("target")
	if target != "panel" && target != "panel-tunnel" && target != "tunnel" && target != "quick" && target != "all" {
		target = "all"
	}
	output, err := m.control("logs", target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: userSafeCommandError(err, output)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": output})
}

func (m *manager) deployQuick(input nativeTunnelInput) (*deployment, error) {
	if err := m.setServiceValues(map[string]string{
		"TUNNEL_ENABLED":       "true",
		"TUNNEL_MODE":          "quick",
		"QUICK_TUNNEL_TARGET":  modeOrigin,
		"QUICK_SERVER_ENABLED": "true",
		"QUICK_SERVER_PORT":    strconv.Itoa(modeServerPort),
		"NATIVE_DEMUX_ENABLED": "false",
		"ACTIVE_MODE":          "mode1",
		"MODE_ENABLED":         "true",
	}); err != nil {
		return nil, err
	}
	input.Transport = "ws"
	input.XHTTPMode = ""
	item, err := m.configureNativeServer(input, "mode1")
	if err != nil {
		return nil, err
	}
	if _, err := m.control("stop-native-demux"); err != nil {
		return nil, errors.New("Could not stop the Mode 2 transport demux.")
	}
	if _, err := m.control("restart-quick-server"); err != nil {
		return nil, errors.New("Could not start the native Quick Tunnel Xray server.")
	}
	if _, err := m.control("restart-tunnel"); err != nil {
		return nil, errors.New("Could not start the temporary Cloudflare Tunnel.")
	}
	host, err := m.waitForQuickHostname(35 * time.Second)
	if err != nil {
		return nil, err
	}
	item.Host = host
	item.Links = buildVLESSLinks(item)
	item.Link = firstLink(item.Links)
	if err := m.saveDeployment(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (m *manager) deployModeTwo(input nativeTunnelInput) (*deployment, error) {
	demuxEnabled := input.Transport == "dual"
	item, err := m.configureNativeServer(input, "mode2")
	if err != nil {
		return nil, err
	}
	if err := m.saveTunnelToken(input.Token, "named"); err != nil {
		return nil, err
	}
	item.Host = input.Domain
	item.Links = buildVLESSLinks(item)
	item.Link = firstLink(item.Links)
	// Save the generated client links before starting services. A transient
	// connector error then remains recoverable from the same Mode 2 card.
	if err := m.saveDeployment(item); err != nil {
		return nil, err
	}
	if err := m.saveModeTwoPreferences(input); err != nil {
		return nil, err
	}
	if err := m.setServiceValues(map[string]string{
		"TUNNEL_ENABLED":          "true",
		"TUNNEL_MODE":             "named",
		"QUICK_SERVER_ENABLED":    "true",
		"QUICK_SERVER_PORT":       strconv.Itoa(modeServerPort),
		"NATIVE_DEMUX_ENABLED":    strconv.FormatBool(demuxEnabled),
		"NATIVE_DEMUX_PORT":       strconv.Itoa(modeServerPort),
		"NATIVE_DEMUX_WS_PORT":    strconv.Itoa(modeWSInternalPort),
		"NATIVE_DEMUX_XHTTP_PORT": strconv.Itoa(modeXHTTPInternalPort),
		"ACTIVE_MODE":             "mode2",
		"MODE_ENABLED":            "true",
	}); err != nil {
		return nil, err
	}
	if !demuxEnabled {
		if _, err := m.control("stop-native-demux"); err != nil {
			return nil, errors.New("Could not stop the WebSocket/xHTTP transport demux.")
		}
	}
	if _, err := m.control("restart-quick-server"); err != nil {
		return nil, errors.New("Could not start the native Xray server for the named tunnel.")
	}
	if demuxEnabled {
		if _, err := m.control("restart-native-demux"); err != nil {
			return nil, errors.New("Could not start the WebSocket/xHTTP transport demux.")
		}
	}
	if _, err := m.control("restart-tunnel"); err != nil {
		return nil, errors.New("Could not start the named Cloudflare Tunnel.")
	}
	return item, nil
}

func (m *manager) deployModeThree(input panelTunnelInput) (*deployment, error) {
	item, err := m.configureModeThreeInbound(input)
	if err != nil {
		return nil, err
	}
	// Native Mode 1/2 uses a separate Xray listener and connector. Keep it
	// untouched: a 3x-ui named tunnel can run alongside either native mode.
	if err := m.setServiceValues(map[string]string{
		"PANEL_TUNNEL_ENABLED":   "true",
		"PANEL_TUNNEL_MODE":      "named",
		"PANEL_TUNNEL_TARGET":    m.panelOrigin(),
		"PANEL_DEMUX_ENABLED":    strconv.FormatBool(input.Transport == "dual"),
		"PANEL_DEMUX_PORT":       strconv.Itoa(modeThreeOriginPort),
		"PANEL_DEMUX_WS_PORT":    strconv.Itoa(modeThreeWSInternalPort),
		"PANEL_DEMUX_XHTTP_PORT": strconv.Itoa(modeThreeXHTTPInternalPort),
	}); err != nil {
		return nil, err
	}
	if err := m.savePanelTunnelToken(input.Token); err != nil {
		return nil, err
	}
	if input.Transport == "dual" {
		if _, err := m.control("restart-panel-demux"); err != nil {
			return nil, errors.New("Could not start the Mode 3 WebSocket/xHTTP demux.")
		}
	} else if _, err := m.control("stop-panel-demux"); err != nil {
		return nil, errors.New("Could not stop the Mode 3 WebSocket/xHTTP demux.")
	}
	if _, err := m.control("restart-panel-tunnel"); err != nil {
		return nil, errors.New("Could not start the 3x-ui Cloudflare Tunnel connector.")
	}
	if err := m.saveDeployment(item); err != nil {
		return nil, err
	}
	if err := m.saveModeThreePreferences(input); err != nil {
		return nil, err
	}
	return item, nil
}

// configureModeThreeInbound owns one clearly tagged 3x-ui inbound. It uses
// the same simple defaults as the old named-tunnel workflow: VLESS with a
// selectable transport and a loopback origin owned by the VPN public route.
func (m *manager) configureModeThreeInbound(input panelTunnelInput) (*deployment, error) {
	panel, err := m.newPanelClient()
	if err != nil {
		return nil, err
	}
	if err := panel.login(); err != nil {
		return nil, errors.New("Could not authenticate to the local 3x-ui panel.")
	}
	if err := panel.ensureAndroidDNS(); err != nil {
		return nil, err
	}
	items, err := panel.listInbounds()
	if err != nil {
		return nil, err
	}

	previous, _ := m.loadDeployment()
	var inbound map[string]any
	for _, candidate := range items {
		if modeThreeConfigurationMatches(candidate, input) {
			inbound = candidate
			break
		}
	}
	if input.Transport == "dual" {
		for _, candidate := range items {
			if intValue(candidate["port"]) != modeThreeOriginPort {
				continue
			}
			if inbound != nil && intValue(candidate["id"]) == intValue(inbound["id"]) {
				continue
			}
			return nil, errors.New("Local port 8080 is occupied by another Mode 3 inbound. Move or remove it in 3x-ui before using WS + xHTTP on this Cloudflare route.")
		}
	}

	// A dual endpoint keeps its Cloudflare route fixed at :8080. The actual
	// 3x-ui listeners are private, and the module demux sends WS/xHTTP traffic
	// to the correct listener based on the request's Upgrade header.
	originPort := nextModeThreeOriginPort(items)
	if input.Transport == "dual" {
		originPort = modeThreeOriginPort
	} else if inbound != nil {
		originPort = intValue(inbound["port"])
		if originPort < modeThreeOriginPort {
			originPort = modeThreeOriginPort
		}
	}

	uuid := firstClientUUID(inbound)
	email := firstClientEmail(inbound)
	if uuid == "" {
		uuid, err = randomUUID()
		if err != nil {
			return nil, err
		}
	}
	if email == "" {
		email = "android-mini-server-mode3-" + randomHex(5)
	}
	path := "/vless-" + randomHex(6)
	if inbound != nil {
		if existingPath := modeThreeWebSocketPath(inbound); existingPath != "" {
			path = existingPath
		}
	}
	_, label, _ := splitFakeSNI(input.FakeSNI)
	if label == "" {
		label = friendlySNIName(fakeSNIHost(input.FakeSNI))
	}

	primaryTransport := input.Transport
	if primaryTransport == "dual" {
		primaryTransport = "ws"
	}
	primaryPort := originPort
	if input.Transport == "dual" {
		primaryPort = modeThreeWSInternalPort
	}
	if inbound == nil {
		inbound = newModeThreeInbound(uuid, email, input.Domain, path, label, input.FakeSNI, primaryTransport, input.XHTTPMode, primaryPort, true)
		if err := panel.addInbound(inbound); err != nil {
			if errors.Is(err, errInboundPortInUse) {
				return nil, errors.New("The required Mode 3 local port is already in use. Move or remove the conflicting 3x-ui inbound first.")
			}
			return nil, err
		}
		items, err = panel.listInbounds()
		if err != nil {
			return nil, err
		}
		inbound = findModeThreeInbound(items, uuid, path, "ws")
		if inbound == nil {
			return nil, errors.New("3x-ui created the Mode 3 inbound but it could not be read back.")
		}
	} else {
		// The panel owns clients, quotas, expiry and Sub IDs after bootstrap.
		// Re-running a matching endpoint keeps that client collection intact.
		existingSettings := inbound["settings"]
		applyModeThreeSettings(inbound, uuid, email, input.Domain, path, label, input.FakeSNI, primaryTransport, input.XHTTPMode, primaryPort, true)
		if existingSettings != nil {
			inbound["settings"] = existingSettings
		}
		id := intValue(inbound["id"])
		if id == 0 {
			return nil, errors.New("The existing managed inbound has no ID.")
		}
		if err := panel.updateInbound(id, inbound); err != nil {
			return nil, err
		}
	}

	if input.Transport == "dual" {
		items, err = panel.listInbounds()
		if err != nil {
			return nil, err
		}
		companion := findModeThreeInbound(items, uuid, path, "xhttp")
		if companion == nil {
			companion = newModeThreeInbound(uuid, email, input.Domain, path, label, input.FakeSNI, "xhttp", input.XHTTPMode, modeThreeXHTTPInternalPort, false)
			if err := panel.addInbound(companion); err != nil {
				if errors.Is(err, errInboundPortInUse) {
					return nil, errors.New("The private xHTTP port for Mode 3 is already in use.")
				}
				return nil, err
			}
			items, err = panel.listInbounds()
			if err != nil {
				return nil, err
			}
			companion = findModeThreeInbound(items, "", path, "xhttp")
		}
		if companion == nil || intValue(companion["id"]) == 0 {
			return nil, errors.New("Could not create the private xHTTP inbound for Mode 3.")
		}
		existingSettings := companion["settings"]
		applyModeThreeSettings(companion, uuid, email, input.Domain, path, label, input.FakeSNI, "xhttp", input.XHTTPMode, modeThreeXHTTPInternalPort, false)
		if existingSettings != nil {
			companion["settings"] = existingSettings
		}
		if err := panel.updateInbound(intValue(companion["id"]), companion); err != nil {
			return nil, err
		}
		if err := panel.attachClient(email, []int{intValue(companion["id"])}); err != nil {
			return nil, err
		}
	}

	if err := panel.restartXray(); err != nil {
		return nil, err
	}
	if err := panel.syncModeThreeHosts(uuid, input.Domain, input.FakeSNI, previous); err != nil {
		return nil, err
	}
	subscriptionID := firstClientSubID(inbound)
	item := &deployment{
		Mode:             "mode3",
		Host:             input.Domain,
		Path:             path,
		UUID:             uuid,
		Label:            label,
		FakeSNI:          input.FakeSNI,
		PortMode:         input.PortMode,
		Transport:        input.Transport,
		XHTTPMode:        input.XHTTPMode,
		CountryCode:      "",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		SubscriptionHost: input.SubscriptionDomain,
		SubscriptionID:   subscriptionID,
		OriginPort:       originPort,
		ClientEmail:      email,
	}
	if input.PanelDomain != "" {
		item.PanelURL = "https://" + input.PanelDomain + "/"
	}
	if input.SubscriptionDomain != "" && subscriptionID != "" {
		item.SubscriptionURL = "https://" + input.SubscriptionDomain + "/sub/" + subscriptionID
	}
	item.Links = buildVLESSLinks(item)
	item.Link = firstLink(item.Links)
	if err := panel.syncModeThreeExternalLinks(email, item.Links); err != nil {
		return nil, err
	}
	return item, nil
}

// 3x-ui normalizes an inbound tag to values such as in-80-tcp, so the custom
// tag used at creation cannot be relied on after its first Xray restart. The
// persisted Mode 3 UUID/path identifies the managed endpoint safely instead.
func isModeThreeInbound(inbound map[string]any, previous *deployment) bool {
	if stringValue(inbound["tag"]) == managerTag {
		return true
	}
	if previous == nil || previous.Mode != "mode3" {
		return false
	}
	if previous.UUID != "" && firstClientUUID(inbound) == previous.UUID {
		return true
	}
	return previous.Path != "" && modeThreeWebSocketPath(inbound) == previous.Path
}

// A changed public hostname, fake SNI, or transport is a separate endpoint.
// Reusing only an identical endpoint protects existing users and subscriptions.
func modeThreeConfigurationMatches(inbound map[string]any, input panelTunnelInput) bool {
	stream, _ := inbound["streamSettings"].(map[string]any)
	wantTransport := input.Transport
	if wantTransport == "dual" {
		wantTransport = "ws"
	}
	if normalizeTransport(stringValue(stream["network"])) != wantTransport || stringValue(inbound["shareAddr"]) != fakeSNIHost(input.FakeSNI) {
		return false
	}
	if input.Transport == "xhttp" {
		xhttp, _ := stream["xhttpSettings"].(map[string]any)
		if normalizeXHTTPMode(stringValue(xhttp["mode"])) != input.XHTTPMode {
			return false
		}
	} else {
		ws, _ := stream["wsSettings"].(map[string]any)
		if stringValue(ws["host"]) != input.Domain {
			return false
		}
	}
	proxies, _ := stream["externalProxy"].([]any)
	if len(proxies) == 0 {
		return false
	}
	proxy, _ := proxies[0].(map[string]any)
	return stringValue(proxy["dest"]) == fakeSNIHost(input.FakeSNI) && stringValue(proxy["sni"]) == input.Domain
}

func findModeThreeInbound(items []map[string]any, uuid, path, transport string) map[string]any {
	for _, item := range items {
		stream, _ := item["streamSettings"].(map[string]any)
		if normalizeTransport(stringValue(stream["network"])) != transport || modeThreeWebSocketPath(item) != path {
			continue
		}
		if uuid == "" || firstClientUUID(item) == uuid {
			return item
		}
	}
	return nil
}

func nextModeThreeOriginPort(inbounds []map[string]any) int {
	used := map[int]bool{}
	for _, inbound := range inbounds {
		used[intValue(inbound["port"])] = true
	}
	for port := modeThreeOriginPort; port < modeThreeOriginPort+100; port++ {
		if !used[port] {
			return port
		}
	}
	return modeThreeOriginPort + 100
}

func modeThreeWebSocketPath(inbound map[string]any) string {
	stream, _ := inbound["streamSettings"].(map[string]any)
	ws, _ := stream["wsSettings"].(map[string]any)
	if path := stringValue(ws["path"]); path != "" {
		return path
	}
	xhttp, _ := stream["xhttpSettings"].(map[string]any)
	return stringValue(xhttp["path"])
}

func newModeThreeInbound(uuid, email, domain, path, label, fakeSNI, transport, xhttpMode string, originPort int, includeClient bool) map[string]any {
	item := map[string]any{
		"remark":            label,
		"enable":            true,
		"expiryTime":        float64(0),
		"total":             float64(0),
		"trafficReset":      "never",
		"listen":            "127.0.0.1",
		"port":              float64(originPort),
		"protocol":          "vless",
		"tag":               managerTag,
		"shareAddrStrategy": "custom",
		"shareAddr":         fakeSNIHost(fakeSNI),
	}
	applyModeThreeSettings(item, uuid, email, domain, path, label, fakeSNI, transport, xhttpMode, originPort, includeClient)
	return item
}

func applyModeThreeSettings(item map[string]any, uuid, email, domain, path, label, fakeSNI, transport, xhttpMode string, originPort int, includeClient bool) {
	item["remark"] = label
	item["enable"] = true
	item["listen"] = "127.0.0.1"
	item["port"] = float64(originPort)
	item["protocol"] = "vless"
	item["tag"] = managerTag
	item["shareAddrStrategy"] = "custom"
	item["shareAddr"] = fakeSNIHost(fakeSNI)
	clients := []any{}
	if includeClient {
		clients = append(clients, map[string]any{
			"id": uuid, "email": email, "flow": "", "limitIp": float64(0), "totalGB": float64(0), "expiryTime": float64(0), "enable": true, "tgId": float64(0), "subId": randomHex(8), "comment": "", "reset": float64(0),
		})
	}
	item["settings"] = map[string]any{"clients": clients, "decryption": "none"}
	alpn := []any{"http/1.1"}
	if normalizeTransport(transport) == "xhttp" {
		alpn = []any{"h3", "h2"}
	}
	stream := map[string]any{
		"network":  normalizeTransport(transport),
		"security": "none",
		"externalProxy": []any{map[string]any{
			"forceTls": "tls", "dest": fakeSNIHost(fakeSNI), "port": float64(443), "remark": "", "sni": domain, "fingerprint": "chrome", "alpn": alpn, "pinnedPeerCertSha256": []any{},
		}},
	}
	if normalizeTransport(transport) == "xhttp" {
		stream["xhttpSettings"] = map[string]any{"path": path, "mode": normalizeXHTTPMode(xhttpMode)}
	} else {
		stream["wsSettings"] = map[string]any{"acceptProxyProtocol": false, "path": path, "host": domain, "headers": map[string]any{}, "heartbeatPeriod": float64(0)}
	}
	item["streamSettings"] = stream
	item["sniffing"] = map[string]any{"enabled": false}
}

// configureNativeServer owns the native Xray configuration used by modes 1 and
// 2. It never calls the 3x-ui API or alters its inbounds.
func (m *manager) configureNativeServer(input nativeTunnelInput, mode string) (*deployment, error) {
	uuid, err := randomUUID()
	if err != nil {
		return nil, err
	}
	path := "/vless-" + randomHex(6)
	makeInbound := func(transport string, port int) map[string]any {
		streamSettings := map[string]any{"network": transport, "security": "none"}
		if transport == "xhttp" {
			streamSettings["xhttpSettings"] = map[string]any{"path": path, "mode": input.XHTTPMode}
		} else {
			streamSettings["wsSettings"] = map[string]any{"path": path}
		}
		return map[string]any{
			"tag":      "android-mini-server-native-" + transport,
			"listen":   "127.0.0.1",
			"port":     port,
			"protocol": "vless",
			"settings": map[string]any{
				"clients":    []any{map[string]any{"id": uuid, "email": "android-mini-server-quick"}},
				"decryption": "none",
			},
			"streamSettings": streamSettings,
		}
	}
	inbounds := []any{}
	if input.Transport == "dual" {
		inbounds = append(inbounds, makeInbound("ws", modeWSInternalPort), makeInbound("xhttp", modeXHTTPInternalPort))
	} else {
		inbounds = append(inbounds, makeInbound(input.Transport, modeServerPort))
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		// Android does not expose a conventional /etc/resolv.conf to a native
		// Xray process. Use explicit IPv4 resolvers instead of its unusable
		// localhost DNS fallback.
		"dns": map[string]any{
			"servers":       []any{"1.1.1.1", "1.0.0.1", "8.8.8.8"},
			"queryStrategy": "UseIPv4",
		},
		"inbounds": inbounds,
		"outbounds": []any{
			map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIPv4"}},
			map[string]any{"tag": "blocked", "protocol": "blackhole"},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(m.stateDir, "quick-xray.json"), data, 0o600); err != nil {
		return nil, err
	}
	return &deployment{
		Mode:        mode,
		Path:        path,
		UUID:        uuid,
		Label:       input.Label,
		FakeSNI:     input.FakeSNI,
		PortMode:    input.PortMode,
		Transport:   input.Transport,
		XHTTPMode:   input.XHTTPMode,
		CountryCode: input.CountryCode,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (m *manager) configureVLESS(host, path, label, mode string) (*deployment, error) {
	panel, err := m.newPanelClient()
	if err != nil {
		return nil, err
	}
	if err := panel.login(); err != nil {
		return nil, errors.New("Could not authenticate to the local 3x-ui panel.")
	}
	items, err := panel.listInbounds()
	if err != nil {
		return nil, err
	}

	var inbound map[string]any
	var uuid string
	for _, item := range items {
		if stringValue(item["tag"]) == managerTag {
			inbound = item
			uuid = firstClientUUID(item)
			break
		}
	}
	if uuid == "" {
		uuid, err = randomUUID()
		if err != nil {
			return nil, err
		}
	}
	if inbound == nil {
		inbound = newVLESSInbound(uuid, host, path, label)
		if err := panel.addInbound(inbound); err != nil {
			return nil, err
		}
	} else {
		applyVLESSSettings(inbound, uuid, host, path, label)
		id := intValue(inbound["id"])
		if id == 0 {
			return nil, errors.New("The existing managed inbound has no ID.")
		}
		if err := panel.updateInbound(id, inbound); err != nil {
			return nil, err
		}
	}
	if err := panel.restartXray(); err != nil {
		return nil, err
	}
	item := &deployment{
		Mode:      mode,
		Host:      host,
		Path:      path,
		UUID:      uuid,
		Label:     label,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	item.Link = buildVLESSLink(item)
	return item, nil
}

func newVLESSInbound(uuid, host, path, label string) map[string]any {
	item := map[string]any{
		"remark":            "Android Mini Server - " + label,
		"enable":            true,
		"expiryTime":        float64(0),
		"total":             float64(0),
		"trafficReset":      "never",
		"listen":            "127.0.0.1",
		"port":              float64(10080),
		"protocol":          "vless",
		"tag":               managerTag,
		"shareAddrStrategy": "custom",
		"shareAddr":         "",
	}
	applyVLESSSettings(item, uuid, host, path, label)
	return item
}

func applyVLESSSettings(item map[string]any, uuid, host, path, label string) {
	item["remark"] = "Android Mini Server - " + label
	item["enable"] = true
	item["listen"] = "127.0.0.1"
	item["port"] = float64(10080)
	item["protocol"] = "vless"
	item["tag"] = managerTag
	item["shareAddrStrategy"] = "custom"
	item["shareAddr"] = host
	item["settings"] = map[string]any{
		"clients": []any{map[string]any{
			"id": uuid, "email": "android-mini-server", "flow": "", "limitIp": float64(0), "totalGB": float64(0), "expiryTime": float64(0), "enable": true, "tgId": float64(0), "subId": randomHex(8), "comment": "", "reset": float64(0),
		}},
		"decryption": "none",
	}
	item["streamSettings"] = map[string]any{
		"network":  "ws",
		"security": "none",
		"wsSettings": map[string]any{
			"acceptProxyProtocol": false,
			"path":                path,
			"host":                host,
			"headers":             map[string]any{},
			"heartbeatPeriod":     float64(0),
		},
		"externalProxy": []any{map[string]any{
			"forceTls": "tls", "dest": host, "port": float64(443), "remark": "", "sni": host, "fingerprint": "chrome", "alpn": []any{"http/1.1"}, "pinnedPeerCertSha256": []any{},
		}},
	}
	item["sniffing"] = map[string]any{"enabled": false}
}

func firstClientUUID(item map[string]any) string {
	settings, _ := item["settings"].(map[string]any)
	clients, _ := settings["clients"].([]any)
	if len(clients) == 0 {
		return ""
	}
	client, _ := clients[0].(map[string]any)
	return stringValue(client["id"])
}

func firstClientEmail(item map[string]any) string {
	settings, _ := item["settings"].(map[string]any)
	clients, _ := settings["clients"].([]any)
	if len(clients) == 0 {
		return ""
	}
	client, _ := clients[0].(map[string]any)
	return stringValue(client["email"])
}

func firstClientSubID(item map[string]any) string {
	settings, _ := item["settings"].(map[string]any)
	clients, _ := settings["clients"].([]any)
	if len(clients) == 0 {
		return ""
	}
	client, _ := clients[0].(map[string]any)
	return stringValue(client["subId"])
}

func fakeSNIHost(value string) string {
	host, _, ok := splitFakeSNI(value)
	if ok {
		return host
	}
	return ""
}

func (m *manager) status() (*statusResponse, error) {
	values, err := readEnvFile(filepath.Join(m.stateDir, "service.env"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	port := valueOr(values, "PANEL_PORT", strconv.Itoa(panelPortDefault))
	managerPort := valueOr(values, "MANAGER_PORT", "2036")
	basePath := valueOr(values, "PANEL_BASE_PATH", "/")
	if basePath == "auto" || basePath == "" {
		basePath = "/"
	}
	activeMode := valueOr(values, "ACTIVE_MODE", "none")
	if activeMode != "mode1" && activeMode != "mode2" && activeMode != "quick" {
		activeMode = "none"
	}
	status := &statusResponse{
		Panel:            processStatus(filepath.Join(m.stateDir, "run", "x-ui.pid"), "x-ui"),
		PanelTunnel:      processStatus(filepath.Join(m.stateDir, "run", "panel-cloudflared.pid"), "cloudflared"),
		QuickServer:      processStatus(filepath.Join(m.stateDir, "run", "quick-xray.pid"), "xray-linux-arm64"),
		NativeDemux:      processStatus(filepath.Join(m.stateDir, "run", "native-demux.pid"), "demux-listen"),
		PanelDemux:       processStatus(filepath.Join(m.stateDir, "run", "panel-demux.pid"), "demux-listen"),
		Tunnel:           processStatus(filepath.Join(m.stateDir, "run", "cloudflared.pid"), "cloudflared"),
		Manager:          processStatus(filepath.Join(m.stateDir, "run", "manager.pid"), "android-mini-server-manager"),
		PanelPort:        port,
		TunnelMode:       valueOr(values, "TUNNEL_MODE", "named"),
		TunnelConfigured: fileHasContent(filepath.Join(m.stateDir, "token.txt")),
		ActiveMode:       activeMode,
		ManagerURL:       "http://127.0.0.1:" + managerPort + "/",
		ManagerBuild:     managerBuild,
		PanelURL:         "http://127.0.0.1:" + port + ensureLeadingSlash(basePath),
		AndroidRelease:   property("ro.build.version.release"),
		DeviceABI:        property("ro.product.cpu.abi"),
	}
	if credentials, err := readEnvFile(filepath.Join(m.stateDir, "panel-credentials.txt")); err == nil {
		status.PanelUsername = credentials["username"]
		status.PanelPassword = credentials["password"]
	}
	status.QuickHostname = m.modeQuickHostname()
	if host := m.panelTunnelHostname(); host != "" {
		status.PanelTunnelURL = "https://" + host + ensureLeadingSlash(basePath)
	}
	if item, err := m.loadDeployment(); err == nil {
		status.Deployment = item
	}
	status.Deployments = m.loadDeployments()
	status.Saved = m.savedConfigurations()
	// Upgrade existing deployments into the form defaults without rewriting
	// their live configuration. A subscription hostname is deliberately left
	// blank because it cannot be inferred from a connector token.
	if status.Deployment != nil && status.Saved.Mode2 == nil && status.Deployment.Mode == "mode2" {
		status.Saved.Mode2 = &nativeTunnelPreferences{
			Domain: status.Deployment.Host, Label: status.Deployment.Label,
			FakeSNI: status.Deployment.FakeSNI, PortMode: status.Deployment.PortMode,
			Transport: status.Deployment.Transport, XHTTPMode: status.Deployment.XHTTPMode,
			CountryCode: status.Deployment.CountryCode, TokenSaved: fileHasContent(filepath.Join(m.stateDir, "token.txt")),
		}
	}
	if status.Deployment != nil && status.Saved.Mode3 == nil && status.Deployment.Mode == "mode3" {
		status.Saved.Mode3 = &modeThreePreferences{
			Domain: status.Deployment.Host, FakeSNI: status.Deployment.FakeSNI,
			PortMode: status.Deployment.PortMode, Transport: status.Deployment.Transport,
			XHTTPMode:  status.Deployment.XHTTPMode,
			TokenSaved: fileHasContent(filepath.Join(m.stateDir, "panel-tunnel-token.txt")),
		}
	}
	return status, nil
}

func (m *manager) control(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(m.moduleDir, "scripts", "control.sh"), args...)
	command.Env = append(os.Environ(), "MODDIR="+m.moduleDir)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), errors.New("Operation timed out.")
	}
	return string(output), err
}

func (m *manager) panelOrigin() string {
	values, err := readEnvFile(filepath.Join(m.stateDir, "service.env"))
	if err != nil {
		return "http://127.0.0.1:" + strconv.Itoa(panelPortDefault)
	}
	return "http://127.0.0.1:" + valueOr(values, "PANEL_PORT", strconv.Itoa(panelPortDefault))
}

func (m *manager) panelCredentials() (string, string, error) {
	values, err := readEnvFile(filepath.Join(m.stateDir, "panel-credentials.txt"))
	if err != nil {
		return "", "", err
	}
	username, password := values["username"], values["password"]
	if username == "" || password == "" {
		return "", "", errors.New("Panel credentials are incomplete.")
	}
	return username, password, nil
}

func (m *manager) saveTunnelToken(token, mode string) error {
	if !validSecret(token, 32, 4096) {
		return errors.New("Tunnel token is malformed.")
	}
	if err := os.WriteFile(filepath.Join(m.stateDir, "token.txt"), []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return m.setServiceValues(map[string]string{"TUNNEL_ENABLED": "true", "TUNNEL_MODE": mode})
}

func (m *manager) savePanelTunnelToken(token string) error {
	if !validSecret(token, 32, 4096) {
		return errors.New("Cloudflare Tunnel connector token is malformed.")
	}
	return os.WriteFile(filepath.Join(m.stateDir, "panel-tunnel-token.txt"), []byte(token+"\n"), 0o600)
}

func (m *manager) savedToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *manager) savedConfigurations() savedConfigurations {
	var saved savedConfigurations
	_ = readJSONFile(filepath.Join(m.stateDir, "mode2-preferences.json"), &saved.Mode2)
	_ = readJSONFile(filepath.Join(m.stateDir, "mode3-preferences.json"), &saved.Mode3)
	if saved.Mode2 != nil {
		saved.Mode2.TokenSaved = fileHasContent(filepath.Join(m.stateDir, "token.txt"))
	}
	if saved.Mode3 != nil {
		saved.Mode3.TokenSaved = fileHasContent(filepath.Join(m.stateDir, "panel-tunnel-token.txt"))
	}
	return saved
}

func (m *manager) saveModeTwoPreferences(input nativeTunnelInput) error {
	pref := nativeTunnelPreferences{
		Domain: input.Domain, Label: input.Label, FakeSNI: input.FakeSNI,
		PortMode: input.PortMode, Transport: input.Transport,
		XHTTPMode: input.XHTTPMode, CountryCode: input.CountryCode, TokenSaved: true,
	}
	return writeJSONFile(filepath.Join(m.stateDir, "mode2-preferences.json"), pref)
}

func (m *manager) saveModeThreePreferences(input panelTunnelInput) error {
	pref := modeThreePreferences{
		Domain: input.Domain, SubscriptionDomain: input.SubscriptionDomain,
		PanelDomain: input.PanelDomain, FakeSNI: input.FakeSNI,
		PortMode: input.PortMode, Transport: input.Transport,
		XHTTPMode: input.XHTTPMode, TokenSaved: true,
	}
	return writeJSONFile(filepath.Join(m.stateDir, "mode3-preferences.json"), pref)
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func writeJSONFile(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (m *manager) setServiceValues(next map[string]string) error {
	path := filepath.Join(m.stateDir, "service.env")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		key, _, ok := strings.Cut(line, "=")
		if ok {
			if value, replace := next[key]; replace {
				lines = append(lines, key+"="+value)
				seen[key] = true
				continue
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	for key, value := range next {
		if !seen[key] {
			lines = append(lines, key+"="+value)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func (m *manager) waitForQuickHostname(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if host := m.modeQuickHostname(); host != "" {
			return host, nil
		}
		time.Sleep(time.Second)
	}
	return "", errors.New("Cloudflare did not issue a trycloudflare.com hostname. Check the Tunnel log.")
}

func hostnameFromLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	match := regexp.MustCompile(`https://([a-z0-9-]+\.trycloudflare\.com)`).FindAllStringSubmatch(string(data), -1)
	if len(match) == 0 {
		return ""
	}
	return match[len(match)-1][1]
}

func (m *manager) modeQuickHostname() string {
	return hostnameFromLog(filepath.Join(m.stateDir, "log", "cloudflared.log"))
}

func (m *manager) panelTunnelHostname() string {
	return hostnameFromLog(filepath.Join(m.stateDir, "log", "panel-cloudflared.log"))
}

// Quick Tunnel hostnames are intentionally ephemeral. Update the saved Mode 1
// URI after a connector restart; the native Xray server itself stays unchanged.
func (m *manager) reconcileQuickDeployment() {
	for {
		m.mu.Lock()
		item, err := m.loadDeployment()
		host := m.modeQuickHostname()
		if err == nil && item != nil && (item.Mode == "mode1" || item.Mode == "quick") && host != "" && host != item.Host {
			item.Host = host
			item.Links = buildVLESSLinks(item)
			item.Link = firstLink(item.Links)
			err = m.saveDeployment(item)
		}
		m.mu.Unlock()
		time.Sleep(3 * time.Second)
	}
}

func (m *manager) saveDeployment(item *deployment) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.stateDir, "deployment.json"), data, 0o600); err != nil {
		return err
	}
	registry := m.loadDeployments()
	switch item.Mode {
	case "mode1", "quick":
		registry.Mode1 = item
	case "mode2":
		registry.Mode2 = item
	case "mode3":
		// Mode 3 may intentionally leave older inbounds in 3x-ui. The manager
		// is a deployment surface, not an inbound inventory, so show only the
		// most recently created or refreshed endpoint here.
		registry.Mode3 = []*deployment{item}
	}
	return writeJSONFile(filepath.Join(m.stateDir, "deployments.json"), registry)
}

func (m *manager) loadDeployment() (*deployment, error) {
	data, err := os.ReadFile(filepath.Join(m.stateDir, "deployment.json"))
	if err != nil {
		return nil, err
	}
	var item deployment
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *manager) loadDeployments() deploymentRegistry {
	var registry deploymentRegistry
	if readJSONFile(filepath.Join(m.stateDir, "deployments.json"), &registry) == nil {
		if len(registry.Mode3) > 1 {
			registry.Mode3 = []*deployment{registry.Mode3[len(registry.Mode3)-1]}
		}
		return registry
	}
	item, err := m.loadDeployment()
	if err != nil {
		return registry
	}
	switch item.Mode {
	case "mode1", "quick":
		registry.Mode1 = item
	case "mode2":
		registry.Mode2 = item
	case "mode3":
		registry.Mode3 = []*deployment{item}
	}
	return registry
}

type panelClient struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	csrf       string
}

var errInboundPortInUse = errors.New("managed inbound port is already in use")

func (m *manager) newPanelClient() (*panelClient, error) {
	values, err := readEnvFile(filepath.Join(m.stateDir, "panel-credentials.txt"))
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(values["local_url"], "/")
	if base == "" || values["username"] == "" || values["password"] == "" {
		return nil, errors.New("Panel credentials are incomplete.")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &panelClient{baseURL: base, username: values["username"], password: values["password"], httpClient: &http.Client{Jar: jar, Timeout: 20 * time.Second}}, nil
}

func (p *panelClient) login() error {
	resp, err := p.httpClient.Get(p.baseURL + "/")
	if err != nil {
		return err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	match := regexp.MustCompile(`name="csrf-token" content="([^"]+)"`).FindStringSubmatch(string(body))
	if len(match) != 2 {
		return errors.New("Could not obtain the panel CSRF token.")
	}
	requestBody, _ := json.Marshal(map[string]string{"username": p.username, "password": p.password, "twoFactorCode": ""})
	request, _ := http.NewRequest(http.MethodPost, p.baseURL+"/login", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", match[1])
	response, err := p.httpClient.Do(request)
	if err != nil {
		return err
	}
	loginBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !jsonSuccess(loginBody) {
		return errors.New("Panel login was rejected.")
	}
	csrfResponse, err := p.httpClient.Get(p.baseURL + "/csrf-token")
	if err != nil {
		return err
	}
	csrfBody, _ := io.ReadAll(csrfResponse.Body)
	csrfResponse.Body.Close()
	var csrfEnvelope struct {
		Obj string `json:"obj"`
	}
	if csrfResponse.StatusCode != http.StatusOK || json.Unmarshal(csrfBody, &csrfEnvelope) != nil || csrfEnvelope.Obj == "" {
		return errors.New("Could not refresh the panel CSRF token.")
	}
	p.csrf = csrfEnvelope.Obj
	return nil
}

func (p *panelClient) listInbounds() ([]map[string]any, error) {
	data, err := p.request(http.MethodGet, "panel/api/inbounds/list", nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Success bool             `json:"success"`
		Obj     []map[string]any `json:"obj"`
		Msg     string           `json:"msg"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || !envelope.Success {
		return nil, errors.New("Could not read panel inbounds.")
	}
	return envelope.Obj, nil
}

func (p *panelClient) addInbound(item map[string]any) error {
	data, err := p.request(http.MethodPost, "panel/api/inbounds/add", item)
	if strings.Contains(strings.ToLower(string(data)), "port") && strings.Contains(strings.ToLower(string(data)), "already used") {
		return errInboundPortInUse
	}
	if err != nil || !jsonSuccess(data) {
		return errors.New("3x-ui refused to create the managed inbound.")
	}
	return nil
}

func (p *panelClient) updateInbound(id int, item map[string]any) error {
	data, err := p.request(http.MethodPost, "panel/api/inbounds/update/"+strconv.Itoa(id), item)
	if err != nil || !jsonSuccess(data) {
		return errors.New("3x-ui refused to update the managed inbound.")
	}
	return nil
}

type panelExternalLink struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Remark     string `json:"remark"`
	Enable     bool   `json:"enable"`
	ExpiryTime int64  `json:"expiryTime"`
	NamePrefix string `json:"namePrefix,omitempty"`
}

const modeThreeExternalLinkPrefix = "Android Mini Server Mode 3: "

func (p *panelClient) attachClient(email string, inboundIDs []int) error {
	data, err := p.request(http.MethodPost, "panel/api/clients/"+url.PathEscape(email)+"/attach", map[string]any{"inboundIds": inboundIDs})
	if err != nil || !jsonSuccess(data) {
		return errors.New("3x-ui could not attach the Mode 3 client to the xHTTP inbound.")
	}
	return nil
}

func (p *panelClient) modeThreeExternalLinks(email string) ([]panelExternalLink, error) {
	data, err := p.request(http.MethodGet, "panel/api/clients/get/"+url.PathEscape(email), nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			ExternalLinks []panelExternalLink `json:"externalLinks"`
		} `json:"obj"`
	}
	if json.Unmarshal(data, &response) != nil || !response.Success {
		return nil, errors.New("3x-ui could not read the Mode 3 client links.")
	}
	return response.Obj.ExternalLinks, nil
}

// syncModeThreeExternalLinks leaves user-authored rows untouched and replaces
// only links generated by this module. The primary inbound already contributes
// links[0] to the subscription, so external rows begin at links[1].
func (p *panelClient) syncModeThreeExternalLinks(email string, links []string) error {
	existing, err := p.modeThreeExternalLinks(email)
	if err != nil {
		return err
	}
	next := make([]panelExternalLink, 0, len(existing)+len(links))
	for _, item := range existing {
		if !strings.HasPrefix(item.Remark, modeThreeExternalLinkPrefix) {
			next = append(next, item)
		}
	}
	for _, link := range links[1:] {
		remark := modeThreeExternalLinkPrefix + vlessLinkRemark(link)
		next = append(next, panelExternalLink{Kind: "link", Value: link, Remark: remark, Enable: true, ExpiryTime: 0})
	}
	data, err := p.request(http.MethodPost, "panel/api/clients/"+url.PathEscape(email)+"/externalLinks", map[string]any{"externalLinks": next})
	if err != nil || !jsonSuccess(data) {
		return errors.New("3x-ui could not save the generated Mode 3 external links.")
	}
	return nil
}

func vlessLinkRemark(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Fragment == "" {
		return "VLESS"
	}
	remark, err := url.QueryUnescape(parsed.Fragment)
	if err != nil || remark == "" {
		return "VLESS"
	}
	return remark
}

// 3x-ui migrates legacy externalProxy entries into Hosts. Those rows take
// precedence in subscriptions, so updating only the inbound leaves stale links.
func (p *panelClient) syncModeThreeHosts(uuid, domain, fakeSNI string, previous *deployment) error {
	inbounds, err := p.listInbounds()
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		if firstClientUUID(inbound) != uuid {
			continue
		}
		id := intValue(inbound["id"])
		data, err := p.request(http.MethodGet, "panel/api/hosts/byInbound/"+strconv.Itoa(id), nil)
		if err != nil {
			return err
		}
		var response struct {
			Success bool             `json:"success"`
			Obj     []map[string]any `json:"obj"`
		}
		if json.Unmarshal(data, &response) != nil || !response.Success {
			return errors.New("Could not read Mode 3 subscription Hosts.")
		}
		for _, group := range response.Obj {
			if !modeThreeHostGroup(group, id, domain, previous) {
				continue
			}
			group["hosts"] = []string{fakeSNIHost(fakeSNI) + ":443"}
			group["port"] = 443
			group["security"] = "tls"
			group["sni"] = domain
			group["overrideSniFromAddress"] = false
			group["keepSniBlank"] = false
			data, err = p.request(http.MethodPost, "panel/api/hosts/update/"+url.PathEscape(stringValue(group["groupId"])), group)
			if err != nil || !jsonSuccess(data) {
				return errors.New("Could not update Mode 3 subscription Host address.")
			}
		}
		return nil
	}
	return errors.New("Could not find the saved Mode 3 inbound.")
}

func modeThreeHostGroup(group map[string]any, inboundID int, domain string, previous *deployment) bool {
	ids, _ := group["inboundIds"].([]any)
	hosts, _ := group["hosts"].([]any)
	// Leave user-created Hosts and groups shared with other inbounds untouched.
	if len(ids) != 1 || intValue(ids[0]) != inboundID || len(hosts) != 1 || !strings.HasPrefix(stringValue(group["remark"]), "imported ") {
		return false
	}
	address := stringValue(hosts[0])
	if address == fakeTikTokSNI+":443" || address == "vnpt.theworkpc.com:443" || address == domain+":443" {
		return true
	}
	if previous == nil || previous.Mode != "mode3" {
		return false
	}
	return address == previous.Host+":443" || address == fakeSNIHost(previous.FakeSNI)+":443"
}

func (p *panelClient) restartXray() error {
	data, err := p.request(http.MethodPost, "panel/api/server/restartXrayService", map[string]any{})
	if err != nil || !jsonSuccess(data) {
		return errors.New("The inbound was saved but Xray could not be restarted.")
	}
	return nil
}

func (p *panelClient) ensureAndroidDNS() error {
	data, err := p.request(http.MethodPost, "panel/api/xray/", map[string]any{})
	if err != nil {
		return err
	}
	var envelope struct {
		Success bool   `json:"success"`
		Obj     string `json:"obj"`
	}
	var settings struct {
		Config  map[string]any `json:"xraySetting"`
		TestURL string         `json:"outboundTestUrl"`
	}
	if json.Unmarshal(data, &envelope) != nil || !envelope.Success || json.Unmarshal([]byte(envelope.Obj), &settings) != nil || settings.Config == nil {
		return errors.New("Could not read 3x-ui DNS settings.")
	}
	if !applyAndroidDNS(settings.Config) {
		return nil
	}
	config, err := json.Marshal(settings.Config)
	if err != nil {
		return err
	}
	form := url.Values{"xraySetting": {string(config)}, "outboundTestUrl": {settings.TestURL}}
	req, err := http.NewRequest(http.MethodPost, p.baseURL+"/panel/api/xray/update", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", p.csrf)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || !jsonSuccess(data) {
		return errors.New("Could not save Android-compatible 3x-ui DNS settings.")
	}
	return nil
}

func applyAndroidDNS(config map[string]any) bool {
	changed := false
	dns, _ := config["dns"].(map[string]any)
	if dns == nil {
		dns = map[string]any{}
	}
	servers, _ := dns["servers"].([]any)
	if len(servers) == 0 {
		dns["servers"] = []any{"1.1.1.1", "1.0.0.1", "8.8.8.8"}
		dns["queryStrategy"] = "UseIPv4"
		config["dns"] = dns
		changed = true
	}
	// AsIs invokes Go's system resolver, which falls back to [::1]:53 on
	// native Android. Use Xray's explicit DNS for the default direct outbound.
	outbounds, _ := config["outbounds"].([]any)
	for _, value := range outbounds {
		outbound, _ := value.(map[string]any)
		if outbound["tag"] != "direct" || outbound["protocol"] != "freedom" {
			continue
		}
		settings, _ := outbound["settings"].(map[string]any)
		if settings == nil {
			settings = map[string]any{}
		}
		if strategy := stringValue(settings["domainStrategy"]); strategy == "" || strategy == "AsIs" {
			settings["domainStrategy"] = "UseIPv4"
			outbound["settings"] = settings
			changed = true
		}
	}
	return changed
}

func (p *panelClient) request(method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, p.baseURL+"/"+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", p.csrf)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return data, fmt.Errorf("Panel API returned HTTP %d", response.StatusCode)
	}
	return data, nil
}

type cloudflareClient struct {
	token  string
	client *http.Client
}

func newCloudflareClient(token string) *cloudflareClient {
	return &cloudflareClient{token: token, client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *cloudflareClient) createTunnel(accountID, name string) (string, string, error) {
	var result struct {
		ID string `json:"id"`
	}
	if err := c.request(http.MethodPost, "/accounts/"+accountID+"/cfd_tunnel", map[string]any{"name": name, "config_src": "cloudflare"}, &result); err != nil {
		return "", "", err
	}
	if !idPattern.MatchString(result.ID) {
		return "", "", errors.New("Cloudflare returned an invalid tunnel ID.")
	}
	var token string
	if err := c.request(http.MethodGet, "/accounts/"+accountID+"/cfd_tunnel/"+result.ID+"/token", nil, &token); err != nil {
		return "", "", err
	}
	if !validSecret(token, 32, 4096) {
		return "", "", errors.New("Cloudflare did not return a usable connector token.")
	}
	return result.ID, token, nil
}

func (c *cloudflareClient) findZone(domain string) (string, error) {
	var result []struct {
		ID string `json:"id"`
	}
	if err := c.request(http.MethodGet, "/zones?name="+url.QueryEscape(domain), nil, &result); err != nil || len(result) != 1 || !validID(result[0].ID) {
		return "", errors.New("Zone not found")
	}
	return result[0].ID, nil
}

func (c *cloudflareClient) putTunnelConfig(accountID, tunnelID string, ingress []map[string]any) error {
	return c.request(http.MethodPut, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", map[string]any{"config": map[string]any{"ingress": ingress}}, nil)
}

func (c *cloudflareClient) upsertCNAME(zoneID, name, target string) error {
	var existing []struct {
		ID string `json:"id"`
	}
	err := c.request(http.MethodGet, "/zones/"+zoneID+"/dns_records?type=CNAME&name="+url.QueryEscape(name), nil, &existing)
	if err != nil {
		return err
	}
	body := map[string]any{"type": "CNAME", "name": name, "content": target, "proxied": true, "ttl": 1}
	if len(existing) > 0 {
		return c.request(http.MethodPut, "/zones/"+zoneID+"/dns_records/"+existing[0].ID, body, nil)
	}
	return c.request(http.MethodPost, "/zones/"+zoneID+"/dns_records", body, nil)
}

func (c *cloudflareClient) request(method, path string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, cloudflareAPI+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &envelope) != nil || response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		if len(envelope.Errors) > 0 && envelope.Errors[0].Message != "" {
			return errors.New("Cloudflare: " + envelope.Errors[0].Message)
		}
		return fmt.Errorf("Cloudflare API returned HTTP %d", response.StatusCode)
	}
	if result != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

func buildVLESSLink(item *deployment) string { return firstLink(buildVLESSLinks(item)) }

func buildVLESSLinks(item *deployment) []string {
	if item == nil || item.UUID == "" || item.Host == "" {
		return nil
	}
	transports := []string{normalizeTransport(item.Transport)}
	if item.Transport == "dual" {
		transports = []string{"ws", "xhttp"}
	}
	portMode := normalizePortMode(item.PortMode)
	links := []string{}
	for _, rawEntry := range strings.Split(item.FakeSNI, ",") {
		sni, remark, ok := splitFakeSNI(rawEntry)
		if !ok {
			continue
		}
		label := remark
		if label == "" {
			label = friendlySNIName(sni)
		}
		if item.CountryCode != "" {
			label = "[" + item.CountryCode + "] " + label
		}
		for _, transport := range transports {
			for _, port := range portsForMode(portMode) {
				values := url.Values{}
				values.Set("encryption", "none")
				values.Set("type", transport)
				values.Set("path", item.Path)
				values.Set("host", item.Host)
				if transport == "xhttp" {
					values.Set("mode", normalizeXHTTPMode(item.XHTTPMode))
				}
				if port == 443 {
					values.Set("security", "tls")
					values.Set("sni", item.Host)
					values.Set("fp", "chrome")
					if transport == "xhttp" {
						values.Set("alpn", "h3,h2")
					} else {
						values.Set("alpn", "http/1.1")
					}
				} else {
					values.Set("security", "none")
				}
				transportLabel := ""
				if len(transports) > 1 {
					transportLabel = " " + strings.ToUpper(transport)
				}
				links = append(links, "vless://"+item.UUID+"@"+sni+":"+strconv.Itoa(port)+"?"+values.Encode()+"#"+url.QueryEscape(label+transportLabel+" "+strconv.Itoa(port)))
			}
		}
	}
	return links
}

func firstLink(links []string) string {
	if len(links) == 0 {
		return ""
	}
	return links[0]
}

func portsForMode(mode string) []int {
	switch mode {
	case "80":
		return []int{80}
	case "443":
		return []int{443}
	default:
		return []int{443, 80}
	}
}

func friendlySNIName(host string) string {
	switch host {
	case fakeTikTokSNI:
		return fakeTikTokLabel
	case fakeVinaSNI:
		return fakeVinaLabel
	default:
		return host
	}
}

func serviceCommand(service, action string) (string, bool) {
	commands := map[string]string{
		"panel:start": "start-panel", "panel:stop": "stop-panel", "panel:restart": "restart-panel",
		"panelTunnel:start": "start-panel-tunnel", "panelTunnel:stop": "stop-panel-tunnel", "panelTunnel:restart": "restart-panel-tunnel",
		"quick:start": "start-quick-server", "quick:stop": "stop-quick-server", "quick:restart": "restart-quick-server",
		"mode:start": "start-mode", "mode:stop": "stop-mode", "mode:restart": "restart-mode",
		"tunnel:start": "start-tunnel", "tunnel:stop": "stop-tunnel", "tunnel:restart": "restart-tunnel",
		"all:start": "start", "all:stop": "stop", "all:restart": "restart",
	}
	command, ok := commands[service+":"+action]
	return command, ok
}

func processStatus(pidFile, expected string) serviceStatus {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return serviceStatus{}
	}
	pid := strings.TrimSpace(string(data))
	if _, err := strconv.Atoi(pid); err != nil {
		return serviceStatus{}
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil || !strings.Contains(string(cmdline), expected) {
		return serviceStatus{}
	}
	return serviceStatus{Running: true, PID: pid}
}

func property(name string) string {
	output, err := exec.Command("getprop", name).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func readEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("Invalid request body.")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "Method not allowed."})
}
func jsonSuccess(data []byte) bool {
	var value struct {
		Success bool `json:"success"`
	}
	return json.Unmarshal(data, &value) == nil && value.Success
}
func userSafeCommandError(err error, output string) string {
	if strings.Contains(output, "invalid") {
		return "The requested service configuration is invalid."
	}
	return err.Error()
}
func validSecret(value string, min, max int) bool {
	value = strings.TrimSpace(value)
	return len(value) >= min && len(value) <= max && !strings.ContainsAny(value, "\r\n\x00")
}
func validID(value string) bool { return regexp.MustCompile(`^[a-zA-Z0-9-]{8,64}$`).MatchString(value) }
func validPort(value int) bool  { return value >= 1 && value <= 65535 }
func normalizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Android Mini Server"
	}
	if len(value) > 48 {
		return value[:48]
	}
	return value
}
func normalizeTunnelName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "android-mini-server"
	}
	if len(value) > 100 {
		return value[:100]
	}
	return value
}
func normalizePrefix(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`).MatchString(value) {
		return value
	}
	return fallback
}
func normalizePortMode(value string) string {
	switch strings.TrimSpace(value) {
	case "80", "443", "both":
		return strings.TrimSpace(value)
	default:
		return "both"
	}
}
func normalizeTransport(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "xhttp") {
		return "xhttp"
	}
	return "ws"
}
func normalizeNativeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dual", "ws+xhttp", "ws,xhttp", "websocket,xhttp", "xhttp,ws", "xhttp,websocket":
		return "dual"
	case "xhttp":
		return "xhttp"
	default:
		return "ws"
	}
}
func normalizeXHTTPMode(value string) string {
	switch strings.TrimSpace(value) {
	case "stream-up", "stream-one", "packet-up":
		return strings.TrimSpace(value)
	default:
		return "packet-up"
	}
}
func normalizeCountryCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if regexp.MustCompile(`^[A-Z]{2}$`).MatchString(value) {
		return value
	}
	return ""
}
func normalizeAllowedFakeSNI(value string) string {
	allowed := map[string]string{
		fakeTikTokSNI: fakeTikTokLabel,
		fakeVinaSNI:   fakeVinaLabel,
	}
	entries := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(value, ",") {
		host, label, ok := splitFakeSNI(raw)
		if !ok || allowed[host] != label || seen[host] {
			return ""
		}
		seen[host] = true
		entries = append(entries, host+"#"+label)
	}
	if len(entries) == 0 || len(entries) > 2 {
		return ""
	}
	return strings.Join(entries, ",")
}
func splitFakeSNI(value string) (string, string, bool) {
	host, remark, _ := strings.Cut(strings.TrimSpace(value), "#")
	host = strings.ToLower(strings.TrimSpace(host))
	remark = strings.TrimSpace(remark)
	if !domainPattern.MatchString(host) || len(remark) > 48 {
		return "", "", false
	}
	return host, remark, true
}
func valueOr(values map[string]string, key, fallback string) string {
	if values[key] != "" {
		return values[key]
	}
	return fallback
}
func fileHasContent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
func ensureLeadingSlash(value string) string {
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}
func stringValue(value any) string { text, _ := value.(string); return text }
func intValue(value any) int       { number, _ := value.(float64); return int(number) }

func randomHex(bytesLen int) string {
	buffer := make([]byte, bytesLen)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}

func randomUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16]), nil
}
