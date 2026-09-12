package platform

import (
	"reflect"
	"testing"
)

func TestBrowserCommands(t *testing.T) {
	u := "https://archiveofourown.org/works/1/chapters/2"
	for _, tc := range []struct {
		os, command string
		args        []string
	}{
		{"darwin", "/usr/bin/open", []string{u}},
		{"linux", "xdg-open", []string{u}},
		{"windows", "rundll32.exe", []string{"url.dll,FileProtocolHandler", u}},
	} {
		cmd, args, err := browserCommand(tc.os, u)
		if err != nil || cmd != tc.command || !reflect.DeepEqual(args, tc.args) {
			t.Fatal(cmd, args, err)
		}
	}
	for _, u := range []string{"file:///etc/passwd", "https://evil.test", "https://archiveofourown.org/works/1?x=1", "https://archiveofourown.org/works/1#frag", "http://fanfiction.net/s/1/1"} {
		if _, _, err := browserCommand("darwin", u); err == nil {
			t.Fatal("accepted", u)
		}
	}
	if _, _, err := browserCommand("ios", u); err == nil {
		t.Fatal("accepted unsupported platform")
	}
}
