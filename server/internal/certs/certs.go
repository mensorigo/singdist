// Package certs manages the panel's TLS certificate versions: an on-disk
// store of imported certificate/key pairs under the data dir, exactly one of
// which is active and served to new TLS handshakes through GetCertificate.
// Hot-swapping is done by replacing an atomic pointer, so activation never
// requires a restart and in-flight connections are never disturbed.
//
// Layout under <dataDir>/tls-store/:
//
//	versions/<id>/cert.pem   leaf + chain, as uploaded
//	versions/<id>/key.pem    private key, mode 0600
//	versions/<id>/meta.json  {id, note, source, importedAt}
//	active.txt               id of the active version
package certs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Info is the API view of one stored certificate version.
type Info struct {
	ID          string   `json:"id"`
	Note        string   `json:"note"`
	Source      string   `json:"source"`
	ImportedAt  int64    `json:"importedAt"`
	Active      bool     `json:"active"`
	CommonName  string   `json:"commonName"`
	DNSNames    []string `json:"dnsNames"`
	Issuer      string   `json:"issuer"`
	Serial      string   `json:"serial"`
	NotBefore   int64    `json:"notBefore"`
	NotAfter    int64    `json:"notAfter"`
	DaysLeft    int      `json:"daysLeft"`
	Fingerprint string   `json:"fingerprint"`
	KeyType     string   `json:"keyType"`
	KeyMatches  bool     `json:"keyMatches"`
	SelfSigned  bool     `json:"selfSigned"`
	Expired     bool     `json:"expired"`
}

// Error is a typed failure that carries the API status/code the handler
// layer should respond with.
// Error is a typed failure that carries the API status/code the handler
// layer should respond with.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }
func errInvalidPEM() *Error {
	return &Error{Status: 400, Code: "invalid_pem", Message: "certificate or private key is not valid PEM"}
}
func errKeyMismatch() *Error {
	return &Error{Status: 400, Code: "key_mismatch", Message: "private key does not match the certificate"}
}
func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}
func errNotFound(id string) *Error {
	return &Error{Status: 404, Code: "cert_not_found", Message: fmt.Sprintf("certificate version %q not found", id)}
}
func errActive() *Error {
	return &Error{Status: 400, Code: "cert_active", Message: "the active certificate cannot be deleted; activate another version first"}
}

// metaFile is the on-disk form of a version's metadata.
// Manager owns the certificate store and the currently served certificate.
type Manager struct {
	mu          sync.Mutex
	versionsDir string
	activeFile  string
	activeID    string // guarded by mu
	current     atomic.Pointer[tls.Certificate]
}

// NewManager creates the store layout under <dataDir>/tls-store/ if needed.
// NewManager creates the store layout under <dataDir>/tls-store/ if needed.
func NewManager(dataDir string) (*Manager, error) {
	storeDir := filepath.Join(dataDir, "tls-store")
	versionsDir := filepath.Join(storeDir, "versions")
	if err := os.MkdirAll(versionsDir, 0o700); err != nil {
		return nil, fmt.Errorf("certs: create store: %w", err)
	}
	return &Manager{
		versionsDir: versionsDir,
		activeFile:  filepath.Join(storeDir, "active.txt"),
	}, nil
}

// Bootstrap makes sure a certificate is loaded and served. On first run (an
// empty store) the pair httpx resolved at startup is imported as the initial
// version and activated; later runs simply load the stored active version.
// Bootstrap makes sure a certificate is loaded and served. On first run (an
// empty store) the pair httpx resolved at startup is imported as the initial
// version and activated; later runs simply load the stored active version.
func (m *Manager) Bootstrap(certFile, keyFile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids, err := m.versionIDs()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		certPEM, err := os.ReadFile(certFile)
		if err != nil {
			return fmt.Errorf("certs: read bootstrap certificate: %w", err)
		}
		keyPEM, err := os.ReadFile(keyFile)
		if err != nil {
			return fmt.Errorf("certs: read bootstrap key: %w", err)
		}
		_, err = m.addLocked(certPEM, keyPEM, "", "bootstrap", true)
		return err
	}

	activeID := m.readActiveID()
	if activeID == "" || !m.exists(activeID) {
		// Fall back to the most recently imported version.
		metas := make([]metaFile, 0, len(ids))
		for _, id := range ids {
			if meta, err := m.readMeta(id); err == nil {
				metas = append(metas, meta)
			}
		}
		if len(metas) == 0 {
			return fmt.Errorf("certs: store has versions but none are readable")
		}
		sort.Slice(metas, func(i, j int) bool { return metas[i].ImportedAt.After(metas[j].ImportedAt) })
		activeID = metas[0].ID
	}
	return m.activateLocked(activeID)
}

// GetCertificate returns the currently active certificate for every TLS
// handshake (SNI is ignored: the panel serves a single name).
// ActivePEM returns the active version's certificate and key PEM. The
// sync engine embeds them into generated core configs for the TLS
// password protocols (hysteria2 / tuic / anytls / trojan).
func (m *Manager) ActivePEM() (certPEM, keyPEM []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.activeLocked()
	if !ok {
		return nil, nil, fmt.Errorf("certs: no active certificate")
	}
	dir := filepath.Join(m.versionsDir, info.ID)
	certPEM, err = os.ReadFile(filepath.Join(dir, "cert.pem"))
	if err != nil {
		return nil, nil, fmt.Errorf("certs: read active cert.pem: %w", err)
	}
	keyPEM, err = os.ReadFile(filepath.Join(dir, "key.pem"))
	if err != nil {
		return nil, nil, fmt.Errorf("certs: read active key.pem: %w", err)
	}
	return certPEM, keyPEM, nil
}

// List returns every stored version, most recently imported first.
// List returns every stored version, most recently imported first.
func (m *Manager) List() ([]Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

// Active returns the Info of the active version.
// Active returns the Info of the active version.
func (m *Manager) Active() (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked()
}

// Add validates and stores a new certificate version; with activate set it
// also becomes the served certificate immediately.
// Add validates and stores a new certificate version; with activate set it
// also becomes the served certificate immediately.
func (m *Manager) Add(certPEM, keyPEM []byte, note string, activate bool) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(certPEM, keyPEM, note, "upload", activate)
}

// GenerateSelfSigned creates a fresh ECDSA P-256 self-signed server
// certificate for the given names (the first name becomes the CN) and
// stores it as a new version, optionally activating it. Names that parse
// as IP addresses become IP SANs; the rest become DNS SANs. Days is
// clamped to [1, 3650]; zero selects the 365-day default.
// UpdateNote rewrites a version's display note in its meta.json. The note
// is a free-form label (max 120 characters); everything else about the
// version is untouched.
func (m *Manager) UpdateNote(id, note string) (Info, string, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 120 {
		return Info{}, "", errValidation("note must be at most 120 characters")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validID(id) || !m.exists(id) {
		return Info{}, "", errNotFound(id)
	}
	meta, err := m.readMeta(id)
	if err != nil {
		return Info{}, "", err
	}
	summary := ""
	if meta.Note != note {
		summary = "备注 " + meta.Note + "→" + note
	}
	meta.Note = note
	raw, err := json.Marshal(meta)
	if err != nil {
		return Info{}, "", fmt.Errorf("certs: encode meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(m.versionsDir, id, "meta.json"), raw, 0o644); err != nil {
		return Info{}, "", fmt.Errorf("certs: write meta.json: %w", err)
	}
	v, err := m.infoLocked(id)
	return v, summary, err
}

// Activate switches the served certificate to the given version.
// Activate switches the served certificate to the given version.
func (m *Manager) Activate(id string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validID(id) || !m.exists(id) {
		return Info{}, errNotFound(id)
	}
	if err := m.activateLocked(id); err != nil {
		return Info{}, err
	}
	return m.infoLocked(id)
}

// Delete removes a stored version. The active version cannot be deleted.
// Delete removes a stored version. The active version cannot be deleted.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validID(id) || !m.exists(id) {
		return errNotFound(id)
	}
	if id == m.activeID {
		return errActive()
	}
	if err := os.RemoveAll(filepath.Join(m.versionsDir, id)); err != nil {
		return fmt.Errorf("certs: delete version %s: %w", id, err)
	}
	return nil
}

// --- internals (mu held by callers) ---------------------------------------

// defaultNoteLocked generates the standard note for a certificate that
// was stored without one: "CERT-XXXX" with four unambiguous letters or
// digits, collision-checked against existing notes. Callers hold m.mu.
func (m *Manager) addLocked(certPEM, keyPEM []byte, note, source string, activate bool) (Info, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		note = m.defaultNoteLocked()
	}
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return Info{}, errValidation("certPem and keyPem are required")
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		if strings.Contains(err.Error(), "private key does not match") {
			return Info{}, errKeyMismatch()
		}
		return Info{}, errInvalidPEM()
	}

	id := m.newVersionID()
	dir := filepath.Join(m.versionsDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, fmt.Errorf("certs: create version dir: %w", err)
	}
	cleanup := func(err error) (Info, error) {
		_ = os.RemoveAll(dir)
		return Info{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0o644); err != nil {
		return cleanup(fmt.Errorf("certs: write cert.pem: %w", err))
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600); err != nil {
		return cleanup(fmt.Errorf("certs: write key.pem: %w", err))
	}
	meta := metaFile{ID: id, Note: note, Source: source, ImportedAt: time.Now().UTC()}
	raw, err := json.Marshal(meta)
	if err != nil {
		return cleanup(fmt.Errorf("certs: encode meta: %w", err))
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		return cleanup(fmt.Errorf("certs: write meta.json: %w", err))
	}

	if activate {
		if err := m.activateLocked(id); err != nil {
			return cleanup(err)
		}
	}
	return m.infoLocked(id)
}
func (m *Manager) activateLocked(id string) error {
	dir := filepath.Join(m.versionsDir, id)
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, "cert.pem"),
		filepath.Join(dir, "key.pem"),
	)
	if err != nil {
		return fmt.Errorf("certs: load version %s: %w", id, err)
	}
	if len(cert.Certificate) > 0 {
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
			cert.Leaf = leaf
		}
	}
	m.current.Store(&cert)
	m.activeID = id
	if err := os.WriteFile(m.activeFile, []byte(id+"\n"), 0o644); err != nil {
		return fmt.Errorf("certs: write active marker: %w", err)
	}
	return nil
}
