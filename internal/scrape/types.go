package scrape

import (
	"github.com/styxnanda/sailune-go/internal/auth"
	"github.com/styxnanda/sailune-go/internal/model"
)

type Site = model.Site
type Metadata = model.Metadata
type SessionStore = auth.SessionStore

const (
	AO3 = model.AO3
	FFN = model.FFN
)
