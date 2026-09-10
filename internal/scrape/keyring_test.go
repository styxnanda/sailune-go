package scrape

import (
	"github.com/zalando/go-keyring"
	"os"
	"testing"
)

// Tests never access the real OS credential store.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
