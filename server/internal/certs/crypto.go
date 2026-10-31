package certs

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// GetCertificate returns the currently active certificate for every TLS
// handshake (SNI is ignored: the panel serves a single name).
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := m.current.Load()
	if cert == nil {
		return nil, fmt.Errorf("certs: no certificate loaded")
	}
	return cert, nil
}

// ActivePEM returns the active version's certificate and key PEM. The
// password protocols (hysteria2 / tuic / anytls / trojan).
// GenerateSelfSigned creates a fresh ECDSA P-256 self-signed server
// certificate for the given names (the first name becomes the CN) and
// stores it as a new version, optionally activating it. Names that parse
// as IP addresses become IP SANs; the rest become DNS SANs. Days is
// clamped to [1, 3650]; zero selects the 365-day default.
func (m *Manager) GenerateSelfSigned(names []string, days int, note string, activate bool) (Info, error) {
	if len(names) == 0 {
		return Info{}, errValidation("at least one domain name or IP is required")
	}
	if days == 0 {
		days = 365
	}
	if days < 1 || days > 3650 {
		return Info{}, errValidation("days must be between 1 and 3650")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Info{}, fmt.Errorf("certs: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return Info{}, fmt.Errorf("certs: generate serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: names[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Duration(days) * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return Info{}, fmt.Errorf("certs: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Info{}, fmt.Errorf("certs: marshal key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	// An empty note is fine: addLocked assigns the standard CERT-XXXX name.
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(certPEM, keyPEM, note, "selfsigned", activate)
}

// UpdateNote rewrites a version's display note in its meta.json. The note
// is a free-form label (max 120 characters); everything else about the
// version is untouched.
func issuerName(leaf *x509.Certificate) string {
	if leaf.Issuer.CommonName != "" {
		return leaf.Issuer.CommonName
	}
	if len(leaf.Issuer.Organization) > 0 {
		return strings.Join(leaf.Issuer.Organization, ", ")
	}
	return leaf.Issuer.String()
}
func serialHex(leaf *x509.Certificate) string {
	b := leaf.SerialNumber.Bytes()
	if len(b) == 0 {
		return "00"
	}
	return strings.ToUpper(hex.EncodeToString(b))
}
func keyTypeName(leaf *x509.Certificate) string {
	switch pub := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", pub.N.BitLen())
	case *ecdsa.PublicKey:
		name := ""
		if params := pub.Curve.Params(); params != nil {
			name = params.Name
		}
		if name != "" {
			return "ECDSA " + name
		}
		return "ECDSA"
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return "Unknown"
	}
}
