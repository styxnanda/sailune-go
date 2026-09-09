//go:build !windows

package sailune

func unprotectWindows(encrypted []byte) ([]byte, error) { return nil, errCookieDecrypt }
