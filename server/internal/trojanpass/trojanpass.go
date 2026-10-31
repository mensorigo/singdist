// Package trojanpass derives a per-subscription Trojan password.
//
// Trojan identifies users by password, not name, so a shared inbound
// password cannot be split by subscription in traffic accounting.
// Each subscription therefore gets its own password, derived with
// HMAC-SHA256 keyed by the inbound's base password over the
// subscription UUID. Knowing one derived password does not reveal
// the base password or any other subscription's password, and the
// derivation is deterministic, so the panel, its subscription
// renderer, and the node agent all compute the same password
// without storing per-subscription secrets. The agent carries its
// own identical copy because the two Go modules cannot import
// each other; keep both in sync.
package trojanpass

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Password returns the Trojan password for one subscription.
func Password(basePassword, subscriptionUUID string) string {
	mac := hmac.New(sha256.New, []byte(basePassword))
	mac.Write([]byte(subscriptionUUID))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}
