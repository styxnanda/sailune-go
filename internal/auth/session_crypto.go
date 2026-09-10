package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const sessionKeyService = "org.sailune.session-encryption.v1"
const maxEncryptedSession = 6 << 20

type encryptedSession struct {
	Version    int    `json:"version"`
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

var errSessionKey = errors.New("session encryption key unavailable; unlock the OS credential store on the original device; to start over, clear this session and sign in again")

func validKeyID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 32
}

func sessionKey(id string) ([]byte, error) {
	if !validKeyID(id) {
		return nil, errSessionKey
	}
	value, err := keyring.Get(sessionKeyService, id)
	if err != nil {
		return nil, errSessionKey
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, errSessionKey
	}
	return key, nil
}

func sessionAAD(site Site, id string) []byte {
	return []byte("sailune/session/v2/" + string(site) + "/" + id)
}

func sealSession(path string, site Site, plaintext []byte) ([]byte, error) {
	var envelope encryptedSession
	var key []byte
	created := false
	old, err := readSessionEnvelope(path)
	defer clear(old)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(old) > maxEncryptedSession {
		return nil, errors.New("session file is too large; clear it before importing")
	}
	if len(old) > 0 && json.Unmarshal(old, &envelope) == nil && envelope.Version == 2 {
		key, err = sessionKey(envelope.KeyID)
		if err != nil {
			return nil, err
		}
	} else {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		id := make([]byte, 32)
		if _, err = rand.Read(id); err != nil {
			return nil, err
		}
		envelope = encryptedSession{Version: 2, KeyID: hex.EncodeToString(id)}
		if err = keyring.Set(sessionKeyService, envelope.KeyID, base64.StdEncoding.EncodeToString(key)); err != nil {
			clear(key)
			return nil, errors.New("cannot store session encryption key in the OS credential store; no plaintext fallback is available")
		}
		created = true
	}
	defer clear(key)
	// Verify that a newly created key can be retrieved before committing any file.
	if created {
		check, err := sessionKey(envelope.KeyID)
		if err != nil {
			return nil, err
		}
		same := subtle.ConstantTimeCompare(check, key) == 1
		clear(check)
		if !same {
			return nil, errSessionKey
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	envelope.Nonce = make([]byte, gcm.NonceSize())
	if _, err = rand.Read(envelope.Nonce); err != nil {
		return nil, err
	}
	envelope.Ciphertext = gcm.Seal(nil, envelope.Nonce, plaintext, sessionAAD(site, envelope.KeyID))
	return json.Marshal(envelope)
}

func openSession(data []byte, site Site) ([]byte, error) {
	var envelope encryptedSession
	if len(data) > maxEncryptedSession || json.Unmarshal(data, &envelope) != nil {
		return nil, errors.New("invalid encrypted session file; file left untouched")
	}
	if envelope.Version == 1 {
		return nil, errors.New("legacy plaintext session; run auth SITE --migrate-from DIR to encrypt it, or clear and sign in again")
	}
	if envelope.Version != 2 || !validKeyID(envelope.KeyID) {
		return nil, errors.New("invalid encrypted session format; file left untouched")
	}
	key, err := sessionKey(envelope.KeyID)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(envelope.Nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid encrypted session nonce; file left untouched")
	}
	plain, err := gcm.Open(nil, envelope.Nonce, envelope.Ciphertext, sessionAAD(site, envelope.KeyID))
	if err != nil {
		return nil, errors.New("session integrity check failed; file left untouched")
	}
	return plain, nil
}

// DefaultSessionDir is independent of the bookmark path. Keys are kept in the
// OS credential store, never alongside these encrypted files.
func DefaultSessionDir() (string, error) {
	return defaultSessionDir()
}

func sessionSite(path string) Site {
	return Site(filepath.Base(path[:len(path)-len(filepath.Ext(path))]))
}

func readSessionEnvelope(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxEncryptedSession+1))
}
