package sailune

import "testing"

func TestLoginBrowserCommands(t *testing.T) {
	for _, site := range []Site{AO3, FFN} {
		for _, platform := range []string{"darwin", "linux", "windows"} {
			name, args, err := loginBrowserCommand(platform, site)
			u, _ := LoginURL(site)
			if err != nil || name == "" || args[len(args)-1] != u {
				t.Fatalf("%s %s: %s %v %v", platform, site, name, args, err)
			}
		}
	}
	if _, _, err := loginBrowserCommand("darwin", Site("invalid")); err == nil {
		t.Fatal("opened unsupported site")
	}
	if _, _, err := loginBrowserCommand("unknown", AO3); err == nil {
		t.Fatal("accepted unsupported desktop")
	}
}
