package library

import (
	"errors"
	"os"
)

// OrganizeRequest is shared by GUI adapters; binary data travels separately.
type OrganizeRequest struct {
	Action       string     `json:"action"`
	Collection   Collection `json:"collection"`
	CollectionID string     `json:"collection_id"`
	IDs          []int64    `json:"ids"`
	Remove       bool       `json:"remove"`
	Filter       Filter     `json:"filter"`
	Kind         string     `json:"kind"`
	ID           int64      `json:"id"`
	Role         string     `json:"role"`
	Path         string     `json:"path"`
	X            float64    `json:"x"`
	Y            float64    `json:"y"`
	Merge        bool       `json:"merge"`
}

func (l Library) Organize(r OrganizeRequest) (any, error) {
	switch r.Action {
	case "collections":
		return l.Collections()
	case "collection-save":
		return l.SaveCollection(r.Collection)
	case "collection-delete":
		return nil, l.DeleteCollection(r.CollectionID)
	case "membership":
		return nil, l.SetMembership(r.CollectionID, r.IDs, r.Remove)
	case "count":
		return l.Count(r.Filter)
	case "tags":
		return l.Tags(r.Kind)
	case "artwork":
		return l.Artwork(r.ID)
	case "artwork-set":
		f, err := os.Open(r.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return l.SetArtwork(r.ID, r.Role, f, r.X, r.Y)
	case "artwork-remove":
		return nil, l.RemoveArtwork(r.ID, r.Role)
	case "backup-export":
		return nil, l.ExportArchiveFile(r.Path)
	case "backup-import":
		return l.ImportBackupFile(r.Path, r.Merge)
	default:
		return nil, errors.New("unknown library organization action")
	}
}
