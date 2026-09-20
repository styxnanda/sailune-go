package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	sailune "github.com/styxnanda/sailune-go"
	"io"
	"os"
	"strconv"
	"strings"
)

func organize(path, command string, args []string, out, errOut io.Writer) error {
	if path == "" {
		path = os.Getenv("SAILUNE_DATA")
	}
	if path == "" {
		var err error
		path, err = sailune.DefaultLibraryPath()
		if err != nil {
			return err
		}
	}
	lib := sailune.Library{Store: sailune.Store{Path: path}}
	if len(args) == 0 {
		return errors.New("use collection list|show|create|edit|delete|add|remove|preview, or art info|set|remove|export")
	}
	action := args[0]
	if action == "--help" || action == "-h" || action == "help" {
		_, err := fmt.Fprintln(out, `Organization commands:
  collection list
  collection create --name NAME [--kind smart --rules JSON]
  collection edit ID [--name NAME] [--rules JSON]
  collection show ID | delete ID
  collection add ID STORY_ID... | remove ID STORY_ID...
  collection preview --rules JSON
  art info STORY_ID
  art set STORY_ID FILE --role cover|background [--x 0..1 --y 0..1]
  art remove STORY_ID --role cover|background
  art export STORY_ID FILE --role cover|background
All organization commands return JSON. Artwork export refuses overwrite.`)
		return err
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	name := fs.String("name", "", "collection name")
	kind := fs.String("kind", "manual", "manual or smart")
	rules := fs.String("rules", "", "JSON rules: personal/source {tags,all}, site, status, fandom")
	role := fs.String("role", "cover", "cover or background")
	x := fs.Float64("x", .5, "horizontal focal point 0..1")
	y := fs.Float64("y", .5, "vertical focal point 0..1")
	fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	var value any
	var err error
	require := func(n int) error {
		if fs.NArg() != n {
			return fmt.Errorf("%s %s expects %d operands", command, action, n)
		}
		return nil
	}
	if command == "collection" {
		switch action {
		case "list":
			if err = require(0); err != nil {
				return err
			}
			value, err = lib.Collections()
		case "create", "edit":
			var c sailune.Collection
			if action == "edit" {
				if err = require(1); err != nil {
					return err
				}
				cs, e := lib.Collections()
				if e != nil {
					return e
				}
				for _, v := range cs {
					if v.ID == fs.Arg(0) {
						c = v
					}
				}
				if c.ID == "" {
					return sailune.ErrNotFound
				}
			} else {
				if err = require(0); err != nil {
					return err
				}
				c.Kind = *kind
			}
			if *name != "" {
				c.Name = *name
			}
			if *rules != "" {
				d := json.NewDecoder(strings.NewReader(*rules))
				d.DisallowUnknownFields()
				if err = d.Decode(&c.Rules); err != nil {
					return err
				}
			}
			value, err = lib.SaveCollection(c)
		case "delete":
			if err = require(1); err != nil {
				return err
			}
			err = lib.DeleteCollection(fs.Arg(0))
			value = map[string]string{"deleted": fs.Arg(0)}
		case "show":
			if err = require(1); err != nil {
				return err
			}
			value, err = lib.List(sailune.Filter{Collection: fs.Arg(0)})
		case "preview":
			if err = require(0); err != nil {
				return err
			}
			var r sailune.CollectionRules
			if err = json.Unmarshal([]byte(*rules), &r); err != nil {
				return err
			}
			value, err = lib.List(sailune.Filter{Rules: &r})
		case "add", "remove":
			if fs.NArg() < 2 {
				return errors.New("supply collection ID and story IDs")
			}
			ids := []int64{}
			for _, s := range fs.Args()[1:] {
				id, e := strconv.ParseInt(s, 10, 64)
				if e != nil || id < 1 {
					return errors.New("invalid story ID")
				}
				ids = append(ids, id)
			}
			err = lib.SetMembership(fs.Arg(0), ids, action == "remove")
			value = map[string]int{"stories": len(ids)}
		default:
			return errors.New("unknown collection action")
		}
	} else {
		n := 1
		if action == "set" || action == "export" {
			n = 2
		}
		if err = require(n); err != nil {
			return err
		}
		id, e := strconv.ParseInt(fs.Arg(0), 10, 64)
		if e != nil || id < 1 {
			return errors.New("invalid story ID")
		}
		switch action {
		case "info":
			value, err = lib.Artwork(id)
		case "set":
			f, e := os.Open(fs.Arg(1))
			if e != nil {
				return e
			}
			defer f.Close()
			value, err = lib.SetArtwork(id, *role, f, *x, *y)
		case "remove":
			err = lib.RemoveArtwork(id, *role)
			value = map[string]string{"removed": *role}
		case "export":
			arts, e := lib.Artwork(id)
			if e != nil {
				return e
			}
			asset := ""
			for _, a := range arts {
				if a.Role == *role {
					asset = a.AssetID
				}
			}
			if asset == "" {
				return sailune.ErrNotFound
			}
			data, e := lib.ArtworkBytes(asset, false)
			if e != nil {
				return e
			}
			f, e := os.OpenFile(fs.Arg(1), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, err = f.Write(data)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			value = map[string]string{"exported": fs.Arg(1)}
		default:
			return errors.New("unknown artwork action")
		}
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
