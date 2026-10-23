package httpx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// TLSConfig describes where the panel's TLS material comes from.
type TLSConfig struct {
	CertFile string // from SUBDIST_TLS_CERT; may be empty
	KeyFile  string // from SUBDIST_TLS_KEY; may be empty
	DataDir  string // fallback location for the generated self-signed pair
}

// EnsurePairFiles guarantees that a usable TLS key pair exists as files and
// returns their paths: it uses the configured pair, or — when none is
// configured — generates (once) a self-signed pair stored in the data dir.
// The pair is also loaded once here so a bad configured pair fails fast at
// startup. The returned paths are what the certs module imports as its
// bootstrap version; actual serving goes through its GetCertificate.
// Encrypted transport is a product requirement: there is no plaintext mode.
func EnsurePairFiles(log *slog.Logger, cfg TLSConfig) (certFile, keyFile string, err error) {
	certFile, keyFile = cfg.CertFile, cfg.KeyFile
	if certFile == "" && keyFile == "" {
		certFile = filepath.Join(cfg.DataDir, "tls.crt")
		keyFile = filepath.Join(cfg.DataDir, "tls.key")
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			if err := generateSelfSigned(certFile, keyFile); err != nil {
				return "", "", fmt.Errorf("httpx: generate self-signed certificate: %w", err)
			}
			log.Warn("generated self-signed TLS certificate for local use",
				"cert", certFile, "key", keyFile,
				"note", "set SUBDIST_TLS_CERT/SUBDIST_TLS_KEY to use a real certificate")
		}
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return "", "", fmt.Errorf("httpx: load TLS key pair: %w", err)
	}
	return certFile, keyFile, nil
}

func generateSelfSigned(certFile, keyFile string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "SubDist (self-signed)"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", hostname},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return nil
}
