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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
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
type metaFile struct {
	ID         string    `json:"id"`
	Note       string    `json:"note"`
	Source     string    `json:"source"`
	ImportedAt time.Time `json:"importedAt"`
}

// Manager owns the certificate store and the currently served certificate.
type Manager struct {
	mu          sync.Mutex
	versionsDir string
	activeFile  string
	activeID    string // guarded by mu
	current     atomic.Pointer[tls.Certificate]
}

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
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := m.current.Load()
	if cert == nil {
		return nil, fmt.Errorf("certs: no certificate loaded")
	}
	return cert, nil
}

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
func (m *Manager) List() ([]Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

// Active returns the Info of the active version.
func (m *Manager) Active() (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked()
}

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
func (m *Manager) defaultNoteLocked() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	used := map[string]bool{}
	if infos, err := m.listLocked(); err == nil {
		for _, in := range infos {
			used[in.Note] = true
		}
	}
	for attempt := 0; attempt < 16; attempt++ {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			break
		}
		for i, b := range buf {
			buf[i] = alphabet[int(b)%len(alphabet)]
		}
		candidate := "CERT-" + string(buf)
		if !used[candidate] {
			return candidate
		}
	}
	// Practically unreachable fallback: timestamp keeps it unique-ish.
	return fmt.Sprintf("CERT-%d", time.Now().Unix()%100000)
}

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

func (m *Manager) listLocked() ([]Info, error) {
	ids, err := m.versionIDs()
	if err != nil {
		return nil, err
	}
	infos := make([]Info, 0, len(ids))
	for _, id := range ids {
		info, err := m.infoLocked(id)
		if err != nil {
			continue // skip half-written / corrupt entries
		}
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		if infos[i].ImportedAt != infos[j].ImportedAt {
			return infos[i].ImportedAt > infos[j].ImportedAt
		}
		return infos[i].ID > infos[j].ID
	})
	return infos, nil
}

func (m *Manager) activeLocked() (Info, bool) {
	if m.activeID == "" {
		return Info{}, false
	}
	info, err := m.infoLocked(m.activeID)
	if err != nil {
		return Info{}, false
	}
	return info, true
}

// infoLocked parses the stored pair of one version and derives its Info.
func (m *Manager) infoLocked(id string) (Info, error) {
	meta, err := m.readMeta(id)
	if err != nil {
		return Info{}, err
	}
	dir := filepath.Join(m.versionsDir, id)
	certPEM, err := os.ReadFile(filepath.Join(dir, "cert.pem"))
	if err != nil {
		return Info{}, fmt.Errorf("certs: read cert.pem of %s: %w", id, err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "key.pem"))
	if err != nil {
		return Info{}, fmt.Errorf("certs: read key.pem of %s: %w", id, err)
	}
	// tls.X509KeyPair doubles as the key-match check.
	_, pairErr := tls.X509KeyPair(certPEM, keyPEM)
	keyMatches := pairErr == nil

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return Info{}, errInvalidPEM()
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Info{}, errInvalidPEM()
	}

	now := time.Now()
	fp := sha256.Sum256(leaf.Raw)
	fpParts := make([]string, len(fp))
	for i, b := range fp {
		fpParts[i] = fmt.Sprintf("%02x", b)
	}
	dnsNames := leaf.DNSNames
	if dnsNames == nil {
		dnsNames = []string{}
	}
	return Info{
		ID:          meta.ID,
		Note:        meta.Note,
		Source:      meta.Source,
		ImportedAt:  meta.ImportedAt.Unix(),
		Active:      id == m.activeID,
		CommonName:  leaf.Subject.CommonName,
		DNSNames:    dnsNames,
		Issuer:      issuerName(leaf),
		Serial:      serialHex(leaf),
		NotBefore:   leaf.NotBefore.Unix(),
		NotAfter:    leaf.NotAfter.Unix(),
		DaysLeft:    int(math.Floor(float64(leaf.NotAfter.Unix()-now.Unix()) / 86400)),
		Fingerprint: strings.Join(fpParts, ":"),
		KeyType:     keyTypeName(leaf),
		KeyMatches:  keyMatches,
		SelfSigned:  leaf.Issuer.String() == leaf.Subject.String(),
		Expired:     now.After(leaf.NotAfter),
	}, nil
}

func (m *Manager) readMeta(id string) (metaFile, error) {
	raw, err := os.ReadFile(filepath.Join(m.versionsDir, id, "meta.json"))
	if err != nil {
		return metaFile{}, fmt.Errorf("certs: read meta of %s: %w", id, err)
	}
	var meta metaFile
	if err := json.Unmarshal(raw, &meta); err != nil {
		return metaFile{}, fmt.Errorf("certs: parse meta of %s: %w", id, err)
	}
	return meta, nil
}

func (m *Manager) versionIDs() ([]string, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("certs: list versions: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (m *Manager) exists(id string) bool {
	st, err := os.Stat(filepath.Join(m.versionsDir, id))
	return err == nil && st.IsDir()
}

func (m *Manager) readActiveID() string {
	raw, err := os.ReadFile(m.activeFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func (m *Manager) newVersionID() string {
	base := time.Now().UTC().Format("20060102T150405Z")
	if !m.exists(base) {
		return base
	}
	for {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			// rand failure is practically impossible; fall back to time nanos.
			return fmt.Sprintf("%s-%04x", base, time.Now().UnixNano()&0xffff)
		}
		id := base + "-" + hex.EncodeToString(b[:])
		if !m.exists(id) {
			return id
		}
	}
}

// validID restricts version ids (which arrive as URL parameters) to the
// characters the manager itself generates, keeping store paths traversal-free.
func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		ok := (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

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
