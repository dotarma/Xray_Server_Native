package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModeTwoDualTransportUsesPrivateXrayPorts(t *testing.T) {
	m := &manager{stateDir: t.TempDir()}
	item, err := m.configureNativeServer(nativeTunnelInput{
		Label: "Dual", FakeSNI: fakeTikTokSNI + "#" + fakeTikTokLabel,
		PortMode: "both", Transport: "dual", XHTTPMode: "packet-up",
	}, "mode2")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.stateDir, "quick-xray.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Port           int `json:"port"`
			StreamSettings struct {
				Network string `json:"network"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 2 || config.Inbounds[0].Port != modeWSInternalPort || config.Inbounds[0].StreamSettings.Network != "ws" || config.Inbounds[1].Port != modeXHTTPInternalPort || config.Inbounds[1].StreamSettings.Network != "xhttp" {
		t.Fatalf("unexpected dual inbounds: %#v", config.Inbounds)
	}
	item.Host = "vpn.example.com"
	item.FakeSNI = fakeTikTokSNI + "#" + fakeTikTokLabel + "," + fakeVinaSNI + "#" + fakeVinaLabel
	links := buildVLESSLinks(item)
	if len(links) != 8 {
		t.Fatalf("expected two SNI values with WS/xHTTP links for ports 80 and 443, got %d: %#v", len(links), links)
	}
}

func TestNormalizeAllowedFakeSNIAcceptsOnlyKnownPair(t *testing.T) {
	combined := fakeTikTokSNI + "#" + fakeTikTokLabel + "," + fakeVinaSNI + "#" + fakeVinaLabel
	if got := normalizeAllowedFakeSNI(combined); got != combined {
		t.Fatalf("combined SNI = %q", got)
	}
	if got := normalizeAllowedFakeSNI(combined + ",unknown.example.com#Unknown"); got != "" {
		t.Fatalf("unexpected untrusted SNI result %q", got)
	}
}

func TestModeThreeDualUsesWSAsPrimaryAndPrivatePorts(t *testing.T) {
	input := panelTunnelInput{
		Domain:    "vpn.example.com",
		FakeSNI:   fakeTikTokSNI + "#" + fakeTikTokLabel,
		Transport: "dual",
		XHTTPMode: "packet-up",
	}
	primary := newModeThreeInbound("primary-id", "owner", input.Domain, "/vpn", fakeTikTokLabel, input.FakeSNI, "ws", input.XHTTPMode, modeThreeWSInternalPort, true)
	if !modeThreeConfigurationMatches(primary, input) {
		t.Fatal("dual input did not recognize its WebSocket primary inbound")
	}
	companion := newModeThreeInbound("primary-id", "owner", input.Domain, "/vpn", fakeTikTokLabel, input.FakeSNI, "xhttp", input.XHTTPMode, modeThreeXHTTPInternalPort, false)
	if intValue(primary["port"]) != modeThreeWSInternalPort || intValue(companion["port"]) != modeThreeXHTTPInternalPort {
		t.Fatalf("unexpected private ports: ws=%v xhttp=%v", primary["port"], companion["port"])
	}
	if firstClientUUID(companion) != "" {
		t.Fatal("xHTTP companion must start empty before attaching the primary client")
	}
}

func TestModeThreeExternalLinksPreserveManualRows(t *testing.T) {
	var saved []panelExternalLink
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/clients/get/owner":
			// Existing rows: one manual and two old managed formats (both removed).
			w.Write([]byte(`{"success":true,"obj":{"externalLinks":[{"kind":"link","value":"vless://manual@example.com:443?type=ws#manual","remark":"Manual node","enable":true,"expiryTime":0},{"kind":"link","value":"vless://old@example.com:443?type=ws#old","remark":"Android Mini Server Mode 3: old","enable":true,"expiryTime":0},{"kind":"link","value":"vless://old2@example.com:443?type=ws#old","remark":"old","namePrefix":"xray-server-native-mode3","enable":true,"expiryTime":0}]}}`))
		case "/panel/api/clients/owner/externalLinks":
			var body struct {
				ExternalLinks []panelExternalLink `json:"externalLinks"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			saved = body.ExternalLinks
			w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	p := &panelClient{baseURL: server.URL, httpClient: server.Client()}
	links := []string{
		"vless://id@second.example.com:443?type=ws#second",
		"vless://id@third.example.com:443?type=xhttp#third",
	}
	if err := p.syncModeThreeExternalLinks("owner", links); err != nil {
		t.Fatal(err)
	}
	// Manual row is kept; prior generated rows are removed. New rows have an
	// invisible marker but clean remarks for subscription users.
	if len(saved) != 3 {
		t.Fatalf("expected 3 saved external links, got %d: %#v", len(saved), saved)
	}
	if saved[0].Remark != "Manual node" {
		t.Fatalf("manual row should be preserved, got remark %q", saved[0].Remark)
	}
	if saved[1].Value != links[0] || saved[1].Remark != "second" || saved[1].NamePrefix != modeThreeExternalLinkMarker {
		t.Fatalf("first generated row was not saved cleanly: %#v", saved[1])
	}
	if saved[2].Value != links[1] || saved[2].Remark != "third" || saved[2].NamePrefix != modeThreeExternalLinkMarker {
		t.Fatalf("second generated row was not saved cleanly: %#v", saved[2])
	}
}

func TestRenameClientUsesUUIDInsteadOfDatabaseID(t *testing.T) {
	var updated map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/clients/get/old":
			w.Write([]byte(`{"success":true,"obj":{"client":{"id":7,"uuid":"00000000-0000-4000-8000-000000000007","email":"old","subId":"sub-id","enable":true}}}`))
		case "/panel/api/clients/update/old":
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				t.Fatal(err)
			}
			w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	p := &panelClient{baseURL: server.URL, httpClient: server.Client()}
	if err := p.renameClient("old", "Free"); err != nil {
		t.Fatal(err)
	}
	if got := stringValue(updated["id"]); got != "00000000-0000-4000-8000-000000000007" {
		t.Fatalf("update id = %q, want client UUID", got)
	}
	if got := stringValue(updated["email"]); got != "Free" {
		t.Fatalf("updated email = %q", got)
	}
}

func TestTransportDemuxRoutesWebSocketAndXHTTP(t *testing.T) {
	for _, test := range []struct {
		name    string
		request string
		want    string
	}{
		{"websocket", "GET /vpn HTTP/1.1\r\nUpgrade: websocket\r\n\r\n", "ws"},
		{"xhttp", "POST /vpn/session HTTP/1.1\r\nContent-Length: 0\r\n\r\n", "xhttp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ws := testBackend(t, "ws")
			defer ws.Close()
			xhttp := testBackend(t, "xhttp")
			defer xhttp.Close()
			server, client := net.Pipe()
			go proxyTransportConnection(server, ws.Addr().String(), xhttp.Addr().String())
			if _, err := io.WriteString(client, test.request); err != nil {
				t.Fatal(err)
			}
			client.SetReadDeadline(time.Now().Add(2 * time.Second))
			response, err := io.ReadAll(client)
			if err != nil {
				t.Fatal(err)
			}
			if string(response) != test.want {
				t.Fatalf("demux response = %q, want %q", response, test.want)
			}
			client.Close()
		})
	}
}

func testBackend(t *testing.T, response string) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		buffer := make([]byte, 4096)
		_, _ = connection.Read(buffer)
		_, _ = io.WriteString(connection, response)
	}()
	return listener
}

func TestAndroidDNSPreservesRoutingAndIsIdempotent(t *testing.T) {
	var config map[string]any
	json.Unmarshal([]byte(`{"outbounds":[{"tag":"direct","protocol":"freedom","settings":{"domainStrategy":"AsIs","finalRules":[{"action":"block"}]}},{"tag":"warp","protocol":"wireguard"}],"routing":{"domainStrategy":"AsIs"}}`), &config)
	if !applyAndroidDNS(config) {
		t.Fatal("default Android DNS was not configured")
	}
	if applyAndroidDNS(config) {
		t.Fatal("second application changed the configuration")
	}
	outbounds := config["outbounds"].([]any)
	settings := outbounds[0].(map[string]any)["settings"].(map[string]any)
	if settings["domainStrategy"] != "UseIPv4" || len(settings["finalRules"].([]any)) != 1 || len(outbounds) != 2 {
		t.Fatal("existing outbound settings were not preserved")
	}
	if config["routing"].(map[string]any)["domainStrategy"] != "AsIs" {
		t.Fatal("routing changed")
	}
	config["dns"] = map[string]any{"servers": []any{"https://dns.example/dns-query"}, "queryStrategy": "UseIP"}
	if applyAndroidDNS(config) {
		t.Fatal("custom DNS should be preserved")
	}
}

func TestModeThreeMigratedHostSubscriptionAddress(t *testing.T) {
	updated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/inbounds/list":
			w.Write([]byte(`{"success":true,"obj":[{"id":1,"settings":{"clients":[{"id":"client-id"}]}}]}`))
		case "/panel/api/hosts/byInbound/1":
			w.Write([]byte(`{"success":true,"obj":[{"groupId":"migrated","inboundIds":[1],"hosts":["old.example.com:443"],"remark":"imported 1","port":443,"security":"tls","sni":"old.example.com"},{"groupId":"custom","inboundIds":[1],"hosts":["custom.example.com:443"],"remark":"User host"}]}`))
		case "/panel/api/hosts/update/migrated":
			var group map[string]any
			if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
				t.Fatal(err)
			}
			if group["hosts"].([]any)[0] != fakeTikTokSNI+":443" || group["sni"] != "new.example.com" || group["security"] != "tls" {
				t.Fatalf("wrong subscription endpoint: %#v", group)
			}
			updated = true
			w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected API mutation: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := &panelClient{baseURL: server.URL, httpClient: server.Client()}
	if err := p.syncModeThreeHosts("client-id", "new.example.com", fakeTikTokSNI+"#"+fakeTikTokLabel, &deployment{Mode: "mode3", Host: "old.example.com"}); err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("migrated Host was not updated")
	}
}

// TestModeThreeInboundRemarkHasNoModePrefix verifies that inbound remarks use
// only the primary Fake SNI and identify the distinct 443 WS/xHTTP inbounds.
func TestModeThreeInboundRemarkHasNoModePrefix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fakeSNI   string
		transport string
		wantWS    string
		wantXHTTP string
	}{
		{
			name:      "single TikTok SNI",
			fakeSNI:   fakeTikTokSNI + "#" + fakeTikTokLabel,
			transport: "ws",
			wantWS:    fakeTikTokLabel + " WS 443",
		},
		{
			name:      "dual SNI WS+XHTTP",
			fakeSNI:   fakeTikTokSNI + "#" + fakeTikTokLabel + "," + fakeVinaSNI + "#" + fakeVinaLabel,
			transport: "dual",
			wantWS:    fakeTikTokLabel + " WS 443",
			wantXHTTP: fakeTikTokLabel + " XHTTP 443",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primaryTransport := tc.transport
			if primaryTransport == "dual" {
				primaryTransport = "ws"
			}
			primaryRemark := modeThreeInboundRemark(tc.fakeSNI, primaryTransport)
			companionRemark := modeThreeInboundRemark(tc.fakeSNI, "xhttp")
			for _, remark := range []string{primaryRemark, companionRemark} {
				if strings.Contains(remark, "Mode 3") {
					t.Errorf("remark %q must not contain 'Mode 3'", remark)
				}
				if strings.Contains(remark, "android-mini-server") || strings.Contains(remark, "Xray Server Native") {
					t.Errorf("remark %q must not contain product name", remark)
				}
				if strings.Contains(remark, fakeTikTokSNI) || strings.Contains(remark, fakeVinaSNI) {
					t.Errorf("remark %q must not contain raw hostname", remark)
				}
			}
			if tc.wantWS != "" && primaryRemark != tc.wantWS {
				t.Errorf("WS remark = %q, want %q", primaryRemark, tc.wantWS)
			}
			if tc.wantXHTTP != "" && companionRemark != tc.wantXHTTP {
				t.Errorf("XHTTP remark = %q, want %q", companionRemark, tc.wantXHTTP)
			}
		})
	}
}

func TestModeThreeInfoInboundUsesReservedLoopbackPort(t *testing.T) {
	inbound := newModeThreeInfoInbound("00000000-0000-4000-8000-000000000001", "Free", "Takeshi.dev", true)
	if intValue(inbound["port"]) != modeThreeInfoPort || stringValue(inbound["listen"]) != "127.0.0.1" || stringValue(inbound["remark"]) != "Takeshi.dev" {
		t.Fatalf("unexpected information inbound: %#v", inbound)
	}
	if firstClientEmail(inbound) != "Free" || firstClientUUID(inbound) == "" {
		t.Fatal("information inbound did not retain the subscription client")
	}
	stream, _ := inbound["streamSettings"].(map[string]any)
	if stringValue(stream["network"]) != "tcp" {
		t.Fatalf("information inbound transport = %q, want tcp", stream["network"])
	}
}

// TestModeThreeConcurrentDeployReturns409 verifies that a second Mode 3 deploy
// request is rejected with HTTP 409 immediately while one is already in flight,
// and that the busy flag clears via defer once the first call ends.
func TestModeThreeConcurrentDeployReturns409(t *testing.T) {
	m := &manager{stateDir: t.TempDir(), moduleDir: t.TempDir()}
	m.mu.Lock()
	defer m.mu.Unlock()

	body := `{"domain":"vpn.example.com","subscriptionDomain":"sub.example.com","panelDomain":"","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","fakeSni":"` + fakeTikTokSNI + "#" + fakeTikTokLabel + `","portMode":"both","transport":"ws","xhttpMode":"packet-up"}`
	req := httptest.NewRequest(http.MethodPost, "/api/deploy/mode3", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.handleModeThreeDeploy(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected HTTP 409 for concurrent deploy, got %d", w.Code)
	}

}

func TestModeThreeDeployUnlocksAfterFailure(t *testing.T) {
	m := &manager{stateDir: t.TempDir(), moduleDir: t.TempDir()}
	body := `{"domain":"vpn.example.com","subscriptionDomain":"sub.example.com","panelDomain":"","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","fakeSni":"` + fakeTikTokSNI + "#" + fakeTikTokLabel + `","portMode":"both","transport":"ws","xhttpMode":"packet-up"}`
	req := httptest.NewRequest(http.MethodPost, "/api/deploy/mode3", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.handleModeThreeDeploy(w, req)
	if !m.mu.TryLock() {
		t.Fatal("operation lock was not released after failed deployment")
	}
	m.mu.Unlock()
}

// TestModeThreeHostSyncNoImportedRowIsNotFatal verifies that syncModeThreeHosts
// succeeds when the inbound exists but has no matching imported Host row,
// since the panel may not create that row until Xray restarts.
func TestModeThreeHostSyncNoImportedRowIsNotFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/inbounds/list":
			// Inbound exists with the matching client UUID.
			w.Write([]byte(`{"success":true,"obj":[{"id":7,"settings":{"clients":[{"id":"test-uuid"}]}}]}`))
		case "/panel/api/hosts/byInbound/7":
			// No imported rows; only user-created Host with non-matching remark.
			w.Write([]byte(`{"success":true,"obj":[{"groupId":"user","inboundIds":[7],"hosts":["user.example.com:443"],"remark":"User row"}]}`))
		default:
			// Any update request is unexpected in this scenario.
			t.Errorf("unexpected API call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := &panelClient{baseURL: server.URL, httpClient: server.Client()}
	// Should succeed: inbound found, no matching imported row is acceptable.
	if err := p.syncModeThreeHosts("test-uuid", "vpn.example.com", fakeTikTokSNI+"#"+fakeTikTokLabel, nil); err != nil {
		t.Fatalf("syncModeThreeHosts should not fail when no imported Host row exists: %v", err)
	}
}

// TestModeThreeHostSyncUpdatesAllMatchingInbounds verifies that when both the
// WS primary and the xHTTP companion inbound share the same client UUID,
// syncModeThreeHosts updates the matching imported Host row for each inbound.
func TestModeThreeHostSyncUpdatesAllMatchingInbounds(t *testing.T) {
	updatedGroups := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/inbounds/list":
			// Two inbounds with the same client UUID (WS primary + xHTTP companion).
			w.Write([]byte(`{"success":true,"obj":[{"id":10,"settings":{"clients":[{"id":"shared-uuid"}]}},{"id":11,"settings":{"clients":[{"id":"shared-uuid"}]}}]}`))
		case "/panel/api/hosts/byInbound/10":
			w.Write([]byte(`{"success":true,"obj":[{"groupId":"g10","inboundIds":[10],"hosts":["old.example.com:443"],"remark":"imported 1","port":443,"security":"tls","sni":"old.example.com"}]}`))
		case "/panel/api/hosts/byInbound/11":
			w.Write([]byte(`{"success":true,"obj":[{"groupId":"g11","inboundIds":[11],"hosts":["old.example.com:443"],"remark":"imported 2","port":443,"security":"tls","sni":"old.example.com"}]}`))
		case "/panel/api/hosts/update/g10":
			updatedGroups["g10"] = true
			w.Write([]byte(`{"success":true}`))
		case "/panel/api/hosts/update/g11":
			updatedGroups["g11"] = true
			w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected API call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := &panelClient{baseURL: server.URL, httpClient: server.Client()}
	prev := &deployment{Mode: "mode3", Host: "old.example.com"}
	if err := p.syncModeThreeHosts("shared-uuid", "vpn.example.com", fakeTikTokSNI+"#"+fakeTikTokLabel, prev); err != nil {
		t.Fatalf("syncModeThreeHosts failed: %v", err)
	}
	if !updatedGroups["g10"] || !updatedGroups["g11"] {
		t.Fatalf("not all inbound Host groups were updated: %v", updatedGroups)
	}
}

// TestModeThreeDualSNIProducesEightLinks verifies that two allowed Fake SNI
// entries combined with WS+xHTTP transport and both public ports 443 and 80
// produce exactly eight unique links.
func TestModeThreeDualSNIProducesEightLinks(t *testing.T) {
	item := &deployment{
		Mode:      "mode3",
		Host:      "vpn.example.com",
		Path:      "/vless-abc123",
		UUID:      "00000000-0000-4000-8000-000000000001",
		FakeSNI:   fakeTikTokSNI + "#" + fakeTikTokLabel + "," + fakeVinaSNI + "#" + fakeVinaLabel,
		PortMode:  "both",
		Transport: "dual",
		XHTTPMode: "packet-up",
	}
	links := buildVLESSLinks(item)
	if len(links) != 8 {
		t.Fatalf("Mode 3 dual SNI: expected 8 unique links (2 SNI × 2 transport × 2 port), got %d:\n%s",
			len(links), strings.Join(links, "\n"))
	}
	// Verify uniqueness.
	seen := map[string]bool{}
	for _, link := range links {
		if seen[link] {
			t.Fatalf("duplicate link in Mode 3 output: %s", link)
		}
		seen[link] = true
	}
	// Each SNI host must appear in exactly 4 links (2 transports × 2 ports).
	tikTokCount, vinaCount := 0, 0
	for _, link := range links {
		if strings.Contains(link, fakeTikTokSNI) {
			tikTokCount++
		}
		if strings.Contains(link, fakeVinaSNI) {
			vinaCount++
		}
	}
	if tikTokCount != 4 || vinaCount != 4 {
		t.Fatalf("expected 4 TikTok links and 4 Vina links, got %d/%d", tikTokCount, vinaCount)
	}
}

func TestModeThreeFullSubscriptionUsesTwoInboundsAndSixExternalLinks(t *testing.T) {
	item := &deployment{
		Mode: "mode3", Host: "vpn.example.com", Path: "/vless-abc123",
		UUID:     "00000000-0000-4000-8000-000000000001",
		FakeSNI:  fakeTikTokSNI + "#" + fakeTikTokLabel + "," + fakeVinaSNI + "#" + fakeVinaLabel,
		PortMode: "both", Transport: "dual", XHTTPMode: "packet-up",
	}
	all := buildVLESSLinks(item)
	external := modeThreeExternalLinks(item)
	if len(all) != 8 || len(external) != 6 {
		t.Fatalf("expected 8 total variants and 6 external variants, got %d and %d", len(all), len(external))
	}
	for _, link := range external {
		if strings.Contains(link, fakeTikTokSNI+":443") && (strings.Contains(link, "type=ws") || strings.Contains(link, "type=xhttp")) {
			t.Fatalf("primary 3x-ui inbound leaked into external links: %s", link)
		}
	}
}
