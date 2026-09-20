package library

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"sync"
)

var imageProcessing sync.Mutex

const MaxArtworkInput = 25 << 20

type Artwork struct {
	StoryID int64   `json:"story_id"`
	Role    string  `json:"role"`
	AssetID string  `json:"asset_id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
}

func validRole(role string) bool { return role == "cover" || role == "background" }
func focal(v float64) bool       { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

// jpegOrientation reads the bounded EXIF orientation field only; all metadata is discarded.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 255 || data[1] != 216 {
		return 1
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 255 {
			return 1
		}
		marker := data[pos+1]
		if marker == 218 || marker == 217 {
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if n < 2 || pos+2+n > len(data) {
			return 1
		}
		p := data[pos+4 : pos+2+n]
		pos += n + 2
		if marker != 225 || len(p) < 14 || string(p[:6]) != "Exif\x00\x00" {
			continue
		}
		p = p[6:]
		var order binary.ByteOrder
		if string(p[:2]) == "II" {
			order = binary.LittleEndian
		} else if string(p[:2]) == "MM" {
			order = binary.BigEndian
		} else {
			return 1
		}
		if order.Uint16(p[2:4]) != 42 {
			return 1
		}
		off := int(order.Uint32(p[4:8]))
		if off < 0 || off+2 > len(p) {
			return 1
		}
		count := int(order.Uint16(p[off : off+2]))
		off += 2
		for i := 0; i < count && off+12 <= len(p); i++ {
			e := p[off : off+12]
			off += 12
			if order.Uint16(e[:2]) == 0x112 && order.Uint16(e[2:4]) == 3 && order.Uint32(e[4:8]) == 1 {
				v := int(order.Uint16(e[8:10]))
				if v >= 1 && v <= 8 {
					return v
				}
			}
		}
		return 1
	}
	return 1
}

// sampleResize avoids allocating an oriented full-size copy of a camera image.
func sampleResize(src image.Image, orientation, width, height int, crop image.Rectangle) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	at := func(x, y int) color.Color {
		switch orientation {
		case 2:
			x = w - 1 - x
		case 3:
			x, y = w-1-x, h-1-y
		case 4:
			y = h - 1 - y
		case 5:
			x, y = y, x
		case 6:
			x, y = y, h-1-x
		case 7:
			x, y = w-1-y, h-1-x
		case 8:
			x, y = w-1-y, x
		}
		return src.At(b.Min.X+x, b.Min.Y+y)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Four samples per destination pixel reduce aliasing while bounding work.
			var rr, gg, bb uint32
			for _, dy := range []float64{.25, .75} {
				for _, dx := range []float64{.25, .75} {
					sx := crop.Min.X + min(crop.Dx()-1, int((float64(x)+dx)*float64(crop.Dx())/float64(width)))
					sy := crop.Min.Y + min(crop.Dy()-1, int((float64(y)+dy)*float64(crop.Dy())/float64(height)))
					r, g, b, a := at(sx, sy).RGBA()
					rr += r + (65535 - a)
					gg += g + (65535 - a)
					bb += b + (65535 - a)
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(rr / 4 >> 8), uint8(gg / 4 >> 8), uint8(bb / 4 >> 8), 255})
		}
	}
	return dst
}
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: quality})
	return b.Bytes(), err
}
func optimizeArtwork(data []byte, role string, x, y float64) ([]byte, []byte, int, int, error) {
	imageProcessing.Lock()
	defer imageProcessing.Unlock()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, nil, 0, 0, errors.New("choose a static JPEG or PNG image")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return nil, nil, 0, 0, errors.New("image exceeds 40 megapixels")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, 0, 0, err
	}
	orientation := jpegOrientation(data)
	w, h := cfg.Width, cfg.Height
	if orientation >= 5 {
		w, h = h, w
	}
	crop := image.Rect(0, 0, w, h)
	if role == "cover" {
		cw, ch := w, h
		if float64(w)/float64(h) > 2.0/3 {
			cw = h * 2 / 3
		} else {
			ch = w * 3 / 2
		}
		cw = max(1, cw)
		ch = max(1, ch)
		left := int(float64(w-cw) * x)
		top := int(float64(h-ch) * y)
		crop = image.Rect(left, top, left+cw, top+ch)
	}
	maxW, maxH, capBytes := 1000, 1500, 750*1024
	if role == "background" {
		maxW, maxH, capBytes = 2000, 2000, 1024*1024
	}
	scale := min(1.0, min(float64(maxW)/float64(crop.Dx()), float64(maxH)/float64(crop.Dy())))
	w = max(1, int(float64(crop.Dx())*scale))
	h = max(1, int(float64(crop.Dy())*scale))
	img := sampleResize(src, orientation, w, h, crop)
	var encoded []byte
	for {
		for _, q := range []int{85, 78, 70} {
			encoded, err = encodeJPEG(img, q)
			if err != nil {
				return nil, nil, 0, 0, err
			}
			if len(encoded) <= capBytes {
				break
			}
		}
		if len(encoded) <= capBytes {
			break
		}
		w = max(1, w*4/5)
		h = max(1, h*4/5)
		img = sampleResize(img, 1, w, h, img.Bounds())
	}
	tw := 160
	if role == "background" {
		tw = 480
	}
	ts := min(1.0, float64(tw)/float64(w))
	thumb, err := encodeJPEG(sampleResize(img, 1, max(1, int(float64(w)*ts)), max(1, int(float64(h)*ts)), img.Bounds()), 80)
	return encoded, thumb, w, h, err
}
func assetHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func cleanupArtwork(tx *sql.Tx) error {
	_, err := tx.Exec("DELETE FROM artwork_assets WHERE id NOT IN (SELECT asset_id FROM story_artwork)")
	return err
}
func (l Library) SetArtwork(id int64, role string, r io.Reader, x, y float64) (Artwork, error) {
	var a Artwork
	if !validRole(role) || !focal(x) || !focal(y) {
		return a, errors.New("invalid artwork role or focal point")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxArtworkInput+1))
	if err != nil {
		return a, err
	}
	if len(data) > MaxArtworkInput {
		return a, errors.New("image exceeds 25 MiB")
	}
	data, thumb, w, h, err := optimizeArtwork(data, role, x, y)
	if err != nil {
		return a, err
	}
	a = Artwork{id, role, assetHash(data), x, y, w, h}
	err = l.Store.write(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT OR IGNORE INTO artwork_assets VALUES(?,?,?,?,?)", a.AssetID, w, h, data, thumb)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO story_artwork VALUES(?,?,?,?,?) ON CONFLICT(bookmark_id,role) DO UPDATE SET asset_id=excluded.asset_id,x=excluded.x,y=excluded.y", id, role, a.AssetID, x, y)
		if err != nil {
			return err
		}
		return cleanupArtwork(tx)
	})
	return a, err
}
func (l Library) RemoveArtwork(id int64, role string) error {
	if !validRole(role) {
		return errors.New("invalid artwork role")
	}
	return l.Store.write(func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM story_artwork WHERE bookmark_id=? AND role=?", id, role)
		if err != nil {
			return err
		}
		return cleanupArtwork(tx)
	})
}
func (l Library) Artwork(id int64) ([]Artwork, error) {
	result := []Artwork{}
	db, err := l.Store.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT bookmark_id,role,asset_id,x,y,width,height FROM story_artwork JOIN artwork_assets ON asset_id=artwork_assets.id WHERE bookmark_id=? ORDER BY role", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Artwork
		if err := rows.Scan(&a.StoryID, &a.Role, &a.AssetID, &a.X, &a.Y, &a.Width, &a.Height); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (l Library) ArtworkBytes(asset string, thumbnail bool) ([]byte, error) {
	db, err := l.Store.open(false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	column := "data"
	if thumbnail {
		column = "thumbnail"
	}
	var data []byte
	err = db.QueryRow("SELECT "+column+" FROM artwork_assets WHERE id=?", asset).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

func PreviewArtwork(r io.Reader, role string, x, y float64) ([]byte, error) {
	if !validRole(role) || !focal(x) || !focal(y) {
		return nil, errors.New("invalid artwork framing")
	}
	b, e := io.ReadAll(io.LimitReader(r, MaxArtworkInput+1))
	if e != nil {
		return nil, e
	}
	if len(b) > MaxArtworkInput {
		return nil, errors.New("image exceeds 25 MiB")
	}
	data, _, _, _, e := optimizeArtwork(b, role, x, y)
	return data, e
}
