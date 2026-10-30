package agent

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func testRealityParams(t *testing.T) realityParams {
	t.Helper()
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return realityParams{
		SNI:        "www.microsoft.com",
		PublicKey:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		ShortID:    "abcd1234",
		Flow:       "xtls-rprx-vision",
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidateRealityKeyMismatch(t *testing.T) {
	p := testRealityParams(t)
	// Corrupt the public key (still valid base64url, wrong key).
	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	p.PublicKey = base64.RawURLEncoding.EncodeToString(other.PublicKey().Bytes())
	if _, err := validateParams(ProtocolVLESSReality, mustJSON(t, p)); err == nil {
		t.Fatal("mismatched reality keypair must be rejected")
	}
}

func TestValidateRealityOK(t *testing.T) {
	p := testRealityParams(t)
	out, err := validateParams(ProtocolVLESSReality, mustJSON(t, p))
	if err != nil {
		t.Fatal(err)
	}
	var back realityParams
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.ShortID != "abcd1234" || back.Flow != "xtls-rprx-vision" {
		t.Fatalf("canonical params lost fields: %+v", back)
	}
}


func TestValidatePortTransportConflict(t *testing.T) {
	reserved := map[int]string{2826: "SSH"}
	vless := Inbound{Name: "v", Protocol: ProtocolVLESSReality, Port: 443, Enabled: true,
		Params: mustJSON(t, testRealityParams(t)), Users: []User{{Name: "s1", UUID: "11111111-1111-4111-8111-111111111111"}}}
	hy2 := Inbound{Name: "h", Protocol: ProtocolHysteria2, Port: 443, Enabled: true,
		Params: mustJSON(t, hy2Params{Password: "pw"})}
	// TCP + UDP on 443 coexist (production shape).
	if _, err := validateInbounds([]Inbound{vless, hy2}, reserved); err != nil {
		t.Fatalf("TCP+UDP sharing a port must pass: %v", err)
	}
	// Two TCP inbounds on 443 conflict.
	trojan := Inbound{Name: "t", Protocol: ProtocolTrojan, Port: 443, Enabled: true,
		Params: mustJSON(t, passwordSNIParams{Password: "pw", SNI: "example.com"})}
	if _, err := validateInbounds([]Inbound{vless, trojan}, reserved); err == nil {
		t.Fatal("two TCP inbounds on one port must conflict")
	}
	// Reserved port rejected.
	vless.Port = 2826
	if _, err := validateInbounds([]Inbound{vless}, reserved); err == nil {
		t.Fatal("reserved port must be rejected")
	}
}

func TestValidateUserUUIDRequired(t *testing.T) {
	vless := Inbound{Name: "v", Protocol: ProtocolVLESSReality, Port: 8443, Enabled: true,
		Params: mustJSON(t, testRealityParams(t)), Users: []User{{Name: "s1"}}}
	if _, err := validateInbounds([]Inbound{vless}, map[int]string{}); err == nil {
		t.Fatal("vless user without uuid must be rejected")
	}
}

func TestValidateNoSecretInvention(t *testing.T) {
	// The agent never generates secrets: a reality push without
	// keys, or hy2 without a password, must fail (the panel
	// generates these at create time; the agent must not).
	if _, err := validateParams(ProtocolVLESSReality, mustJSON(t, realityParams{SNI: "example.com"})); err == nil {
		t.Fatal("reality without keys must be rejected, not generated")
	}
	if _, err := validateParams(ProtocolHysteria2, mustJSON(t, hy2Params{})); err == nil {
		t.Fatal("hy2 without password must be rejected, not generated")
	}
}
