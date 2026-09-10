package auth

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Migrate encrypts an existing session, then removes the source only after
// verifying the destination. It refuses to overwrite a different session.
func (s SessionStore) Migrate(site Site, sourceDir string) (SessionStatus, error) {
	source := SessionStore{Dir: sourceDir}
	src, err := source.path(site)
	if err != nil {
		return SessionStatus{}, err
	}
	dst, err := s.path(site)
	if err != nil {
		return SessionStatus{}, err
	}
	src, err = filepath.Abs(src)
	if err != nil {
		return SessionStatus{}, err
	}
	dst, err = filepath.Abs(dst)
	if err != nil {
		return SessionStatus{}, err
	}
	err = source.locked(site, func(_ string) error {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxEncryptedSession+1))
		f.Close()
		if err != nil {
			return err
		}
		defer clear(data)
		var header struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(data, &header) != nil {
			return errors.New("invalid source session")
		}
		if header.Version == 2 {
			data, err = openSession(data, site)
			if err != nil {
				return err
			}
			defer clear(data)
		}
		jar, _, err := decodeSession(data, site)
		if err != nil {
			return err
		}
		migrate := func(path string) error {
			if src != dst {
				if _, err := os.Lstat(path); err == nil {
					return errors.New("destination session exists; clear it explicitly before migration")
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if err := writePrivateJSON(path, sessionFile{Version: 1, Cookies: jar.snapshot()}); err != nil {
				return err
			}
			if _, _, err := loadSession(path, site); err != nil {
				return err
			}
			if src != dst {
				if err := os.Remove(src); err != nil {
					return errors.New("encrypted session saved, but source could not be removed; remove the old source before syncing")
				}
			}
			return nil
		}
		if src == dst {
			return migrate(dst)
		}
		return s.locked(site, migrate)
	})
	if err != nil {
		return SessionStatus{}, err
	}
	return s.Status(site)
}
