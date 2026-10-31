package engine

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxSkinBytes = 2 << 20  // 2 MB
	maxPackBytes = 32 << 20 // 32 MB, same limit the app screen shows
)

type asset struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "skin" or "pack"
	Name string `json:"name"`
	Size int    `json:"size"`
	Slim bool   `json:"slim"`
	File string `json:"file"`
}

type upload struct {
	kind  string
	name  string
	total int
	slim  bool
	buf   []byte
}

func (s *engineState) assetDir() string { return filepath.Join(s.dir, "assets") }

// loadAssets reads the saved skin and pack list. The caller must hold s.mu.
func (s *engineState) loadAssets() {
	b, err := os.ReadFile(filepath.Join(s.dir, "assets.json"))
	if err != nil {
		return
	}
	var items []asset
	if json.Unmarshal(b, &items) == nil {
		s.assets = items
	}
}

// saveAssets writes the list to disk. The caller must hold s.mu.
func (s *engineState) saveAssets() {
	if s.dir == "" {
		return
	}
	if b, err := json.Marshal(s.assets); err == nil {
		_ = os.WriteFile(filepath.Join(s.dir, "assets.json"), b, 0o600)
	}
}

// assetItems returns the list for the app screen. The caller must hold s.mu.
func (s *engineState) assetItems() []asset {
	out := make([]asset, len(s.assets))
	copy(out, s.assets)
	return out
}

func (s *engineState) beginUpload(kind, name string, total int, slim bool) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" {
		return "", "The app is not ready yet. Try again."
	}
	limit := 0
	switch kind {
	case "skin":
		limit = maxSkinBytes
	case "pack":
		limit = maxPackBytes
	default:
		return "", "Unsupported file type."
	}
	if total <= 0 || total > limit {
		return "", "That file is too large or empty."
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		name = kind
	}
	if len(name) > 80 {
		name = name[:80]
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", "Could not start the upload."
	}
	id := hex.EncodeToString(raw)
	if s.uploads == nil {
		s.uploads = map[string]*upload{}
	}
	s.uploads[id] = &upload{kind: kind, name: name, total: total, slim: slim, buf: make([]byte, 0, total)}
	return id, ""
}

func (s *engineState) uploadChunk(id string, offset int, data string) string {
	chunk, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "Could not read the upload data."
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.uploads[id]
	if u == nil {
		return "This upload expired. Try again."
	}
	if offset != len(u.buf) || len(u.buf)+len(chunk) > u.total {
		delete(s.uploads, id)
		return "The upload arrived out of order. Try again."
	}
	u.buf = append(u.buf, chunk...)
	return ""
}

func (s *engineState) finishUpload(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.uploads[id]
	delete(s.uploads, id)
	if u == nil {
		return "This upload expired. Try again."
	}
	if len(u.buf) != u.total {
		return "The upload was incomplete. Try again."
	}
	ext := ".mcpack"
	switch u.kind {
	case "skin":
		cfg, err := png.DecodeConfig(bytes.NewReader(u.buf))
		if err != nil {
			return "That is not a PNG skin."
		}
		if !(cfg.Width >= 64 && cfg.Width%64 == 0 && (cfg.Height == cfg.Width || cfg.Height*2 == cfg.Width)) {
			return "Skin must be a 64x64, 64x32 or HD (128x128) PNG."
		}
		ext = ".png"
	case "pack":
		zr, err := zip.NewReader(bytes.NewReader(u.buf), int64(len(u.buf)))
		if err != nil {
			return "That file is not a Minecraft resource pack."
		}
		found := false
		for _, f := range zr.File {
			n := strings.TrimPrefix(f.Name, "./")
			if n == "manifest.json" || (strings.Count(n, "/") == 1 && strings.HasSuffix(n, "/manifest.json")) {
				found = true
				break
			}
		}
		if !found {
			return "That pack has no manifest.json."
		}
	}
	if err := os.MkdirAll(s.assetDir(), 0o755); err != nil {
		return "Could not save the file."
	}
	file := id + ext
	if err := os.WriteFile(filepath.Join(s.assetDir(), file), u.buf, 0o600); err != nil {
		return "Could not save the file."
	}
	if u.kind == "skin" { // only one custom skin at a time
		kept := s.assets[:0]
		for _, a := range s.assets {
			if a.Kind == "skin" {
				_ = os.Remove(filepath.Join(s.assetDir(), a.File))
				continue
			}
			kept = append(kept, a)
		}
		s.assets = kept
	}
	s.assets = append(s.assets, asset{ID: id, Kind: u.kind, Name: u.name, Size: len(u.buf), Slim: u.slim, File: file})
	s.saveAssets()
	return ""
}

func (s *engineState) removeAsset(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.assets[:0]
	for _, a := range s.assets {
		if a.ID == id {
			_ = os.Remove(filepath.Join(s.assetDir(), a.File))
			continue
		}
		kept = append(kept, a)
	}
	s.assets = kept
	s.saveAssets()
	return ""
}

// packPathsLocked lists saved resource packs. The caller must hold s.mu.
func (s *engineState) packPathsLocked() []string {
	var out []string
	for _, a := range s.assets {
		if a.Kind == "pack" {
			out = append(out, filepath.Join(s.assetDir(), a.File))
		}
	}
	return out
}

func (s *engineState) skinPath() (string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.assets {
		if a.Kind == "skin" {
			return filepath.Join(s.assetDir(), a.File), a.Slim, true
		}
	}
	return "", false, false
}

// lanAddress finds the phone's Wi-Fi address without asking Android for the
// network list (which newer Android versions block). No data is sent.
func lanAddress() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok && a.IP.To4() != nil && !a.IP.IsLoopback() {
		return a.IP.String()
	}
	return ""
}
