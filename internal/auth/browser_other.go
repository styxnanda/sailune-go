//go:build !windows

package auth

func unprotectWindows(encrypted []byte) ([]byte, error) { return nil, errCookieDecrypt }
