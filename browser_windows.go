//go:build windows

package sailune

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

func unprotectWindows(encrypted []byte) ([]byte, error) {
	if len(encrypted) == 0 {
		return nil, errCookieDecrypt
	}
	in := windows.DataBlob{Size: uint32(len(encrypted)), Data: &encrypted[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, errCookieDecrypt
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
