package cli

import (
	"github.com/zalando/go-keyring"
	"os"
	"testing"
)

// Tests never access the real OS credential store.
func TestMain(m *testing.M) {
	keyring.MockInit()
	dir, err := os.MkdirTemp("", "sailune-test-sessions-")
	if err != nil {
		panic(err)
	}
	os.Setenv("SAILUNE_SESSIONS", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
