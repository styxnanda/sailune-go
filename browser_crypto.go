package sailune

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

var errCookieDecrypt = errors.New("cannot decrypt this browser's site cookies; use the same OS account and unlock its credential store, or import from Firefox")
var errAppBound = errors.New("Chromium application-bound (v20) cookies are not supported; use Firefox or a Netscape cookie export; do not disable browser protection")

func chromiumDecryptor(ctx context.Context, browser, profile string) cookieDecryptFunc {
	return newChromiumDecryptor(runtime.GOOS,
		func() ([]byte, error) { return browserSecret(ctx, browser) },
		func() ([]byte, error) { return windowsBrowserKey(profile) },
		unprotectWindows)
}

func newChromiumDecryptor(platform string, secret, windowsKey func() ([]byte, error), dpapi func([]byte) ([]byte, error)) cookieDecryptFunc {
	var key []byte
	return func(host string, encrypted []byte, version int) (string, error) {
		if bytes.HasPrefix(encrypted, []byte("v20")) {
			return "", errAppBound
		}
		var plain []byte
		var err error
		switch platform {
		case "windows":
			if bytes.HasPrefix(encrypted, []byte("v10")) {
				if key == nil {
					key, err = windowsKey()
					if err != nil {
						return "", err
					}
				}
				plain, err = decryptGCM(key, encrypted[3:])
			} else if len(encrypted) > 0 && encrypted[0] != 'v' {
				plain, err = dpapi(encrypted)
			} else {
				return "", errCookieDecrypt
			}
		case "darwin", "linux":
			prefix := string(encrypted[:min(3, len(encrypted))])
			if prefix != "v10" && prefix != "v11" {
				return "", errCookieDecrypt
			}
			if platform == "darwin" && prefix != "v10" {
				return "", errCookieDecrypt
			}
			var password []byte
			iterations := 1
			if platform == "linux" && prefix == "v10" {
				password = []byte("peanuts")
			} else {
				if key == nil {
					password, err = secret()
					if err != nil {
						return "", err
					}
					if platform == "darwin" {
						iterations = 1003
					}
					key, err = pbkdf2.Key(sha1.New, string(password), []byte("saltysalt"), iterations, 16)
					clear(password)
					if err != nil {
						return "", errCookieDecrypt
					}
				}
			}
			cbcKey := key
			if password != nil && platform == "linux" && prefix == "v10" {
				cbcKey, _ = pbkdf2.Key(sha1.New, string(password), []byte("saltysalt"), 1, 16)
			}
			plain, err = decryptCBC(cbcKey, encrypted[3:])
		default:
			return "", errors.New("Chromium decryption is supported on macOS, Linux, and Windows; use Firefox on this platform")
		}
		if err != nil {
			return "", errCookieDecrypt
		}
		return chromiumPlaintext(host, plain, version)
	}
}

func decryptCBC(key, encrypted []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil || len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, errCookieDecrypt
	}
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(plain, encrypted)
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || !bytes.Equal(plain[len(plain)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, errCookieDecrypt
	}
	return plain[:len(plain)-padding], nil
}

func decryptGCM(key, encrypted []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errCookieDecrypt
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(encrypted) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errCookieDecrypt
	}
	return gcm.Open(nil, encrypted[:gcm.NonceSize()], encrypted[gcm.NonceSize():], nil)
}

func chromiumPlaintext(host string, plain []byte, version int) (string, error) {
	if version >= 24 {
		hash := sha256.Sum256([]byte(host))
		if len(plain) < len(hash) || !bytes.Equal(plain[:len(hash)], hash[:]) {
			return "", errCookieDecrypt
		}
		plain = plain[len(hash):]
	}
	if !utf8.Valid(plain) {
		return "", errCookieDecrypt
	}
	return string(plain), nil
}

func browserSecret(ctx context.Context, browser string) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		account := map[string]string{"brave": "Brave", "chrome": "Chrome", "chromium": "Chromium", "edge": "Microsoft Edge", "vivaldi": "Vivaldi"}[browser]
		cmd = exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-w", "-a", account, "-s", account+" Safe Storage")
	} else {
		application := map[string]string{"brave": "brave", "chrome": "chrome", "chromium": "chromium", "edge": "chromium", "vivaldi": "chrome"}[browser]
		cmd = exec.CommandContext(ctx, "secret-tool", "lookup", "application", application)
	}
	// Output stays in memory: it is never inherited by the terminal or logged.
	secret, err := cmd.Output()
	if err != nil || len(secret) == 0 {
		if runtime.GOOS == "linux" {
			return nil, errors.New("cannot read Chromium's Secret Service key; install secret-tool and unlock the login keyring; KWallet is not supported")
		}
		return nil, errors.New("cannot read browser Safe Storage from macOS Keychain; allow the Keychain prompt under the browser's OS account and retry")
	}
	return bytes.TrimSuffix(secret, []byte("\n")), nil
}

func windowsBrowserKey(profile string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(profile), "Local State"))
	if err != nil {
		return nil, errors.New("cannot read Chromium Local State beside the selected profile")
	}
	var state struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil, errCookieDecrypt
	}
	key, err := base64.StdEncoding.DecodeString(state.OSCrypt.EncryptedKey)
	if err != nil || !strings.HasPrefix(string(key), "DPAPI") {
		return nil, errCookieDecrypt
	}
	return unprotectWindows(key[5:])
}
