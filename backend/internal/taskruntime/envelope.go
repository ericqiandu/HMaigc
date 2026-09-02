package taskruntime

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const EnvelopeVersion = 1

var (
	ErrInvalidEnvelope       = errors.New("task envelope is invalid")
	ErrUnknownKey            = errors.New("task envelope signing key is unknown")
	ErrInvalidSignature      = errors.New("task envelope signature is invalid")
	ErrExpired               = errors.New("task envelope has expired")
	ErrClaimMismatch         = errors.New("task envelope claims do not match execution facts")
	ErrPayloadDigestMismatch = errors.New("task envelope payload digest does not match")
)

type Binding struct {
	TenantKind         string
	TenantID           string
	ProjectID          string
	CanvasID           string
	RunID              string
	ArtifactRevisionID string
	TaskID             string
	TaskType           string
	RequesterID        string
}

type Claims struct {
	Version            int    `json:"version"`
	TenantKind         string `json:"tenantKind"`
	TenantID           string `json:"tenantId"`
	ProjectID          string `json:"projectId"`
	CanvasID           string `json:"canvasId"`
	RunID              string `json:"runId"`
	ArtifactRevisionID string `json:"artifactRevisionId"`
	TaskID             string `json:"taskId"`
	TaskType           string `json:"taskType"`
	RequesterID        string `json:"requesterId"`
	PayloadDigest      string `json:"payloadDigest"`
	ExpiresAtUnix      int64  `json:"expiresAtUnix"`
}

type VerificationKeys map[string][]byte

type signedBody struct {
	KeyID  string `json:"keyId"`
	Claims Claims `json:"claims"`
}

type envelope struct {
	KeyID     string `json:"keyId"`
	Claims    Claims `json:"claims"`
	Signature string `json:"signature"`
}

func NewClaims(binding Binding, payload []byte, expiresAt time.Time) Claims {
	return Claims{
		Version:    EnvelopeVersion,
		TenantKind: binding.TenantKind, TenantID: binding.TenantID,
		ProjectID: binding.ProjectID, CanvasID: binding.CanvasID, RunID: binding.RunID,
		ArtifactRevisionID: binding.ArtifactRevisionID, TaskID: binding.TaskID,
		TaskType: binding.TaskType, RequesterID: binding.RequesterID,
		PayloadDigest: PayloadDigest(payload), ExpiresAtUnix: expiresAt.UTC().Unix(),
	}
}

func PayloadDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func Sign(claims Claims, keyID string, key []byte) ([]byte, error) {
	if err := validateKey(keyID, key); err != nil {
		return nil, err
	}
	if err := validateClaims(claims); err != nil {
		return nil, err
	}
	bodyJSON, err := json.Marshal(signedBody{KeyID: keyID, Claims: claims})
	if err != nil {
		return nil, fmt.Errorf("%w: canonical body", ErrInvalidEnvelope)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(bodyJSON)
	encoded, err := json.Marshal(envelope{
		KeyID: keyID, Claims: claims, Signature: hex.EncodeToString(mac.Sum(nil)),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: canonical envelope", ErrInvalidEnvelope)
	}
	return encoded, nil
}

func Verify(encoded []byte, keys VerificationKeys, expected Binding, payload []byte, now time.Time) (Claims, error) {
	claims, err := Authenticate(encoded, keys, payload, now)
	if err != nil {
		return Claims{}, err
	}
	if claims.Binding() != expected {
		return Claims{}, ErrClaimMismatch
	}
	return claims, nil
}

func Authenticate(encoded []byte, keys VerificationKeys, payload []byte, now time.Time) (Claims, error) {
	parsed, err := decodeEnvelope(encoded)
	if err != nil {
		return Claims{}, err
	}
	key, ok := keys[parsed.KeyID]
	if !ok {
		return Claims{}, ErrUnknownKey
	}
	if err := validateKey(parsed.KeyID, key); err != nil {
		return Claims{}, err
	}
	bodyJSON, err := json.Marshal(signedBody{KeyID: parsed.KeyID, Claims: parsed.Claims})
	if err != nil {
		return Claims{}, fmt.Errorf("%w: canonical body", ErrInvalidEnvelope)
	}
	providedSignature, err := hex.DecodeString(parsed.Signature)
	if err != nil || len(providedSignature) != sha256.Size {
		return Claims{}, ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(bodyJSON)
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return Claims{}, ErrInvalidSignature
	}
	if parsed.Claims.ExpiresAtUnix <= 0 || !now.UTC().Before(time.Unix(parsed.Claims.ExpiresAtUnix, 0).UTC()) {
		return Claims{}, ErrExpired
	}
	if parsed.Claims.PayloadDigest != PayloadDigest(payload) {
		return Claims{}, ErrPayloadDigestMismatch
	}
	return parsed.Claims, nil
}

func (claims Claims) Binding() Binding {
	return bindingFromClaims(claims)
}

func decodeEnvelope(encoded []byte) (envelope, error) {
	if len(encoded) == 0 {
		return envelope{}, ErrInvalidEnvelope
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var parsed envelope
	if err := decoder.Decode(&parsed); err != nil {
		return envelope{}, fmt.Errorf("%w: malformed JSON", ErrInvalidEnvelope)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return envelope{}, err
	}
	if err := validateClaims(parsed.Claims); err != nil {
		return envelope{}, err
	}
	if strings.TrimSpace(parsed.KeyID) == "" || parsed.KeyID != strings.TrimSpace(parsed.KeyID) || len(parsed.KeyID) > 128 || strings.TrimSpace(parsed.Signature) == "" {
		return envelope{}, ErrInvalidEnvelope
	}
	canonical, err := json.Marshal(parsed)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return envelope{}, fmt.Errorf("%w: encoding is not canonical", ErrInvalidEnvelope)
	}
	return parsed, nil
}

func validateKey(keyID string, key []byte) error {
	if strings.TrimSpace(keyID) == "" || keyID != strings.TrimSpace(keyID) || len(keyID) > 128 || len(key) < 32 {
		return ErrInvalidEnvelope
	}
	return nil
}

func validateClaims(claims Claims) error {
	if claims.Version != EnvelopeVersion || claims.ExpiresAtUnix <= 0 ||
		(claims.TenantKind != "personal" && claims.TenantKind != "team") || !validFact(claims.TenantID, true) ||
		!validFact(claims.ProjectID, false) || !validFact(claims.CanvasID, false) ||
		!validFact(claims.RunID, false) || !validFact(claims.ArtifactRevisionID, false) ||
		!validFact(claims.TaskID, true) || !validFact(claims.TaskType, true) ||
		!validFact(claims.RequesterID, true) || len(claims.PayloadDigest) != sha256.Size*2 {
		return ErrInvalidEnvelope
	}
	if _, err := hex.DecodeString(claims.PayloadDigest); err != nil {
		return ErrInvalidEnvelope
	}
	return nil
}

func validFact(value string, required bool) bool {
	if value != strings.TrimSpace(value) || len(value) > 256 {
		return false
	}
	return !required || value != ""
}

func bindingFromClaims(claims Claims) Binding {
	return Binding{
		TenantKind: claims.TenantKind, TenantID: claims.TenantID,
		ProjectID: claims.ProjectID, CanvasID: claims.CanvasID, RunID: claims.RunID,
		ArtifactRevisionID: claims.ArtifactRevisionID, TaskID: claims.TaskID,
		TaskType: claims.TaskType, RequesterID: claims.RequesterID,
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing JSON", ErrInvalidEnvelope)
	}
	return nil
}
