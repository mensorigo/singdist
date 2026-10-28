package agent

import (
	"encoding/json"
	"testing"
)

func TestGenerateVLESSRealityShape(t *testing.T) {
	ib := Inbound{Name: "v1", Protocol: ProtocolVLESSReality, Port: 443, Enabled: true,
		Params: mustJSON(t, testRealityParams(t)),
		Users:  []User{{Name: "s1", UUID: "11111111-1111-4111-8111-111111111111"}},
	}
	doc, _, err := generate([]Inbound{ib}, "", nil, "/tmp/x/sing-box.log", "http://127.0.0.1:9090")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	inbounds := docInbounds(root)
	if len(inbounds) != 1 {
		t.Fatalf("want 1 inbound, got %d", len(inbounds))
	}
	gen := inbounds[0]
	if gen["type"] != "vless" || gen["tag"] != "v1" || gen["listen"] != "::" {
		t.Fatalf("bad base: %v", gen)
	}
	if gen["listen_port"].(float64) != 443 {
		t.Fatalf("bad port: %v", gen["listen_port"])
	}
	tls, _ := gen["tls"].(map[string]any)
	reality, _ := tls["reality"].(map[string]any)
	if reality["enabled"] != true || reality["private_key"] == "" {
		t.Fatalf("bad reality block: %v", tls)
	}
	if tls["server_name"] != "www.microsoft.com" {
		t.Fatalf("bad sni: %v", tls["server_name"])
	}
	users, _ := gen["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("want 1 user, got %d", len(users))
	}
	// Skeleton: log output, clash api, direct outbound.
	logBlock, _ := root["log"].(map[string]any)
	if logBlock["output"] != "/tmp/x/sing-box.log" {
		t.Fatalf("bad log output: %v", logBlock)
	}
	exp, _ := root["experimental"].(map[string]any)
	clash, _ := exp["clash_api"].(map[string]any)
	if clash["external_controller"] != "http://127.0.0.1:9090" {
		t.Fatalf("bad clash api: %v", clash)
	}
}

func TestGenerateSkipsDisabledAndUnknown(t *testing.T) {
	off := Inbound{Name: "off", Protocol: ProtocolShadowsocks, Port: 9001, Enabled: false,
		Params: mustJSON(t, ssParams{Method: "aes-256-gcm", Password: "pw"})}
	on := Inbound{Name: "on", Protocol: ProtocolShadowsocks, Port: 9002, Enabled: true,
		Params: mustJSON(t, ssParams{Method: "aes-256-gcm", Password: "pw"})}
	doc, _, err := generate([]Inbound{off, on}, "", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	_ = json.Unmarshal(doc, &root)
	if got := len(docInbounds(root)); got != 1 {
		t.Fatalf("want 1 enabled inbound, got %d", got)
	}
}

func TestGenerateHy2UsesPushedCert(t *testing.T) {
	ib := Inbound{Name: "h1", Protocol: ProtocolHysteria2, Port: 443, Enabled: true,
		Params: mustJSON(t, hy2Params{Password: "pw", UpMbps: 100, DownMbps: 200}),
		Users:  []User{{Name: "s1"}},
	}
	cert := &Cert{Certificate: "CERT-PEM", Key: "KEY-PEM"}
	doc, _, err := generate([]Inbound{ib}, "203.0.113.7", cert, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	_ = json.Unmarshal(doc, &root)
	gen := docInbounds(root)[0]
	tls, _ := gen["tls"].(map[string]any)
	if tls["server_name"] != "203.0.113.7" {
		t.Fatalf("hy2 sni must be the node public host, got %v", tls["server_name"])
	}
	certs, _ := tls["certificate"].([]any)
	if len(certs) != 1 || certs[0] != "CERT-PEM" || tls["key"] != "KEY-PEM" {
		t.Fatalf("hy2 must embed the pushed cert: %v", tls)
	}
	if gen["up_mbps"].(float64) != 100 || gen["down_mbps"].(float64) != 200 {
		t.Fatalf("bad bandwidth: %v", gen)
	}
}
