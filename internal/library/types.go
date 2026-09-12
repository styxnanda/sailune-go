package library

import "github.com/styxnanda/sailune-go/internal/model"

type Bookmark = model.Bookmark
type Metadata = model.Metadata
type MetadataPatch = model.MetadataPatch
type Site = model.Site
type Status = model.Status

const (
	AO3       = model.AO3
	FFN       = model.FFN
	Planned   = model.Planned
	Reading   = model.Reading
	Completed = model.Completed
	OnHold    = model.OnHold
	Dropped   = model.Dropped
)
