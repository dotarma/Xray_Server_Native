package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			w.Write([]byte(`{"success":true,"obj":{"externalLinks":[{"kind":"link","value":"vless://manual@example.com:443?type=ws#manual","remark":"Manual node","enable":true,"expiryTime":0},{"kind":"link","value":"vless://old@example.com:443?type=ws#old","remark":"Android Mini Server Mode 3: old","enable":true,"expiryTime":0}]}}`))
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
		"vless://id@first.example.com:443?type=ws#primary",
		"vless://id@second.example.com:443?type=ws#second",
		"vless://id@third.example.com:443?type=xhttp#third",
	}
	if err := p.syncModeThreeExternalLinks("owner", links); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 3 || saved[0].Remark != "Manual node" || saved[1].Value != links[1] || saved[2].Value != links[2] {
		t.Fatalf("unexpected saved external links: %#v", saved)
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
