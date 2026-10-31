package certs

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// metaFile is the on-disk form of a version's metadata.
type metaFile struct {
	ID         string    `json:"id"`
	Note       string    `json:"note"`
	Source     string    `json:"source"`
	ImportedAt time.Time `json:"importedAt"`
}

// Manager owns the certificate store and the currently served certificate.
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
