// Package secrets keeps secret references (spec 34.1 機密參照) and the master
// key that encrypts them (spec 35.2), for ticket 011.
//
// Shape of the data:
//   - A secret has an id, kind, name, use scope, optional expiry and a list of
//     versions. Each version is one AES-256-GCM ciphertext bound to
//     (secret id, version) and to the master-key generation that sealed it.
//   - The master key lives in a keyring file beside the database, never in a
//     database column. The keyring holds every generation that still has
//     ciphertext referring to it, so a backup taken before a rotation stays
//     readable after it.
//   - Every change is recorded in secret_changes without the value, old or new.
//
// What callers can do:
//   - Create / Rotate a secret value; Reveal it for a consumer that is in the
//     secret's scope; SetScope / SetExpiry / Delete; read Changes.
//   - RotateMasterKey: new generation, every version re-wrapped and verified
//     under both keys inside one transaction; a failure leaves the old key
//     active and the data readable. A crash between the two durable steps is
//     reconciled at the next Open.
//   - HashToken / VerifyToken for verify-only tokens that must not be stored
//     in a recoverable form (34.1 API 存取 Token, 35.2 雜湊優先).
package secrets

import (
	"errors"
	"time"
)

// Kind classifies a secret (35.2 分級管理). Verify-only tokens are not a Kind:
// they are stored as digests via HashToken and never enter this store.
type Kind string

const (
	KindPassword         Kind = "password"
	KindPrivateKey       Kind = "private-key"
	KindAPIToken         Kind = "api-token"
	KindOAuthRefresh     Kind = "oauth-refresh"
	KindBackupCredential Kind = "backup-credential"
	KindGeneric          Kind = "generic"
)

var kinds = map[Kind]bool{
	KindPassword: true, KindPrivateKey: true, KindAPIToken: true,
	KindOAuthRefresh: true, KindBackupCredential: true, KindGeneric: true,
}

// Secret is the metadata of a secret reference. It never carries the value.
type Secret struct {
	ID        string
	Kind      Kind
	Name      string
	Version   int // current version; 0 only transiently during Create
	KeyID     string
	Scope     []string // consumer refs allowed to Reveal, e.g. "service:svc_x"
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Change is one row of the change record. It never carries a value.
type Change struct {
	ID          int64
	SecretID    string
	SecretName  string
	Action      string // created | rotated | scope-changed | expiry-changed | deleted | rewrapped
	FromVersion *int
	ToVersion   *int
	KeyID       string
	Actor       string
	OccurredAt  time.Time
}

// CreateInput is the request for Service.Create.
type CreateInput struct {
	Kind      Kind
	Name      string
	Value     []byte
	Scope     []string
	ExpiresAt *time.Time
	Actor     string
}

// RevealRequest asks for a value. Version 0 means the current version.
type RevealRequest struct {
	ID       string
	Version  int
	Consumer string
}

// RotationResult reports a master-key rotation.
type RotationResult struct {
	OldKeyID  string
	NewKeyID  string
	Rewrapped int // secret versions re-encrypted
}

// VerifyReport is the result of decrypting every stored version.
type VerifyReport struct {
	Checked  int
	Failures []string // "secret_id@version: reason" — never a value
}

// Errors reported by this package. Messages never contain secret values.
var (
	ErrNotFound           = errors.New("secrets: secret not found")
	ErrNameTaken          = errors.New("secrets: a secret with that name already exists")
	ErrEmptyValue         = errors.New("secrets: value must not be empty")
	ErrInvalidKind        = errors.New("secrets: unknown secret kind")
	ErrOutOfScope         = errors.New("secrets: consumer is not in the secret's use scope")
	ErrExpired            = errors.New("secrets: secret has expired")
	ErrKeyNotFound        = errors.New("secrets: data refers to a master key generation the keyring does not have")
	ErrKeyringPermissions = errors.New("secrets: master key file must be readable by its owner only")
	ErrKeyringMissing     = errors.New("secrets: master key file is missing")
	ErrKeyringDamaged     = errors.New("secrets: master key file is unreadable")
	ErrDecrypt            = errors.New("secrets: ciphertext did not authenticate")
)

// RecoveryMaterialSource is what the recovery-bundle exporter (ticket 078,
// DAT-003) needs from this package: the keyring bytes to wrap with the
// recovery key. Keyring implements it. The export itself is not in this ticket.
type RecoveryMaterialSource interface {
	// Snapshot returns the current keyring file content.
	Snapshot() ([]byte, error)
	// RecoveryAcknowledged reports whether an administrator has confirmed
	// they hold the recovery material (35.2 「已取得復原資料」檢查).
	RecoveryAcknowledged() (ok bool, by string, at time.Time)
}
