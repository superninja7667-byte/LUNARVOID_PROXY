// Package engine is the LUNARVOID proxy core. The Android app talks to it
// through one function, Call, which takes and returns JSON.
package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/auth"
	"golang.org/x/oauth2"
)

// settings are the menu switches. Mods are preview toggles that do nothing yet.
type settings struct {
	EmoteMenu bool            `json:"emoteMenu"`
	Mods      map[string]bool `json:"mods"`
}

func (st settings) get(key string) bool {
	if key == "emote" {
		return st.EmoteMenu
	}
	return st.Mods[key]
}

func (st *settings) set(key string, v bool) {
	if key == "emote" {
		st.EmoteMenu = v
		return
	}
	if st.Mods == nil {
		st.Mods = map[string]bool{}
	}
	st.Mods[key] = v
}

func (st settings) clone() settings {
	out := settings{EmoteMenu: st.EmoteMenu, Mods: make(map[string]bool, len(st.Mods))}
	for k, v := range st.Mods {
		out.Mods[k] = v
	}
	return out
}

type authInfo struct {
	State   string // idle, pending, done, error
	Message string
	Link    string
	Code    string
	Err     string
}

type engineState struct {
	mu          sync.Mutex
	dir         string
	state       string // stopped, starting, running, error
	errText     string
	note        string // progress or warning shown under the Start button
	listenPort  int
	settings    settings
	accountName string
	auth        authInfo
	authGen     int
	authCancel  context.CancelFunc
	key         []byte
	xblCache    *auth.XBLTokenCache
	tokenSource oauth2.TokenSource
	gen         int
	run         *runner
	runLan      bool
	uploads     map[string]*upload
	assets      []asset
}

var eng = &engineState{
	state:      "stopped",
	listenPort: 19132,
	settings:   settings{EmoteMenu: true, Mods: map[string]bool{}},
	auth:       authInfo{State: "idle"},
}

type request struct {
	Action   string `json:"action"`
	Dir      string `json:"dir"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Lan      bool   `json:"lan"`
	Mode     string `json:"mode"`
	Key      string `json:"key"`
	Value    bool   `json:"value"`
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Total    int    `json:"total"`
	Slim     bool   `json:"slim"`
	UploadID string `json:"uploadId"`
	Offset   int    `json:"offset"`
	Data     string `json:"data"`
}

// Call runs one JSON request and returns the current status as JSON. The
// requests and the status shape match what the app screen (index.html) sends.
func Call(requestJSON string) (out string) {
	defer func() {
		if rec := recover(); rec != nil {
			b, _ := json.Marshal(map[string]any{"available": true, "state": "stopped", "error": "Internal error: " + panicText(rec)})
			out = string(b)
		}
	}()
	var r request
	if err := json.Unmarshal([]byte(requestJSON), &r); err != nil {
		return `{"available":true,"state":"stopped","error":"bad request"}`
	}
	var assetErr, assetUpload, replayErr string
	switch r.Action {
	case "configure":
		eng.mu.Lock()
		eng.setKeyLocked(r.Key)
		eng.mu.Unlock()
	case "init":
		eng.init(r.Dir)
	case "signin":
		eng.startSignIn()
	case "cancelSignin":
		eng.cancelSignIn()
	case "signout", "removeAccount":
		eng.signOut()
	case "start":
		if r.Mode == "replay" {
			replayErr = "Replay playback is coming in the next build."
		} else {
			eng.start(r.Host, r.Port, r.Lan)
		}
	case "stop":
		eng.stop()
	case "beginUpload":
		assetUpload, assetErr = eng.beginUpload(r.Kind, r.Name, r.Total, r.Slim)
	case "uploadChunk":
		assetErr = eng.uploadChunk(r.UploadID, r.Offset, r.Data)
	case "finishUpload":
		assetErr = eng.finishUpload(r.UploadID)
	case "removeAsset":
		assetErr = eng.removeAsset(r.ID)
	case "previewReplay", "editReplay", "removeReplay", "pauseReplay":
		replayErr = "Replay editing is coming in the next build."
	}
	return eng.statusJSON(assetUpload, assetErr, replayErr)
}

type persisted struct {
	Settings    settings `json:"settings"`
	AccountName string   `json:"accountName"`
}

func (s *engineState) init(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dir == "" || s.dir != "" {
		return
	}
	s.dir = dir
	_ = os.MkdirAll(dir, 0o755)
	s.loadAssets()
	if b, err := os.ReadFile(filepath.Join(dir, "state.json")); err == nil {
		var p persisted
		if json.Unmarshal(b, &p) == nil {
			s.settings = p.Settings.clone()
			s.accountName = p.AccountName
		}
	}
	if t := s.readToken(); t != nil {
		s.tokenSource = xblConfig.RefreshTokenSource(t)
		s.xblCache = xblConfig.NewTokenCache()
		s.auth = authInfo{State: "done"}
		s.writeToken(t) // turns an old unencrypted file into an encrypted one
		go s.refreshProfile()
	}
}

// persist writes settings to disk. The caller must hold s.mu.
func (s *engineState) persist() {
	if s.dir == "" {
		return
	}
	b, err := json.Marshal(persisted{Settings: s.settings, AccountName: s.accountName})
	if err == nil {
		_ = os.WriteFile(filepath.Join(s.dir, "state.json"), b, 0o600)
	}
}

func (s *engineState) snapshot() settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.clone()
}

func (s *engineState) toggle(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.set(key, !s.settings.get(key))
	s.persist()
}

func (s *engineState) setAccountName(name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountName != name {
		s.accountName = name
		s.persist()
	}
}

func (s *engineState) setNote(gen int, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen {
		s.note = text
	}
}

func (s *engineState) current(gen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen == gen
}

func (s *engineState) statusJSON(uploadID, assetErr, replayErr string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	players := 0
	if s.run != nil {
		players = s.run.count()
	}
	state := s.state
	switch state {
	case "running":
		state = "listening"
		if players > 0 {
			state = "connected"
		}
	case "error":
		state = "stopped"
	}
	authState := s.auth.State
	if authState == "done" || authState == "error" {
		authState = "idle"
	}
	accounts := []map[string]any{}
	if s.tokenSource != nil {
		name := s.accountName
		if name == "" {
			name = "Microsoft account"
		}
		accounts = append(accounts, map[string]any{"id": "main", "name": name, "selected": true})
	}
	address := "127.0.0.1"
	if s.runLan {
		if ip := lanAddress(); ip != "" {
			address = ip
		}
	}
	warning := s.note
	if warning == "" && len(s.key) == 0 {
		warning = "Secure storage is unavailable, so your sign-in is saved without extra encryption."
	}
	assets := map[string]any{"items": s.assetItems()}
	if uploadID != "" {
		assets["uploadId"] = uploadID
	}
	if assetErr != "" {
		assets["error"] = assetErr
	}
	replay := map[string]any{"recording": false, "playing": false, "paused": false, "clips": []any{}}
	if replayErr != "" {
		replay["error"] = replayErr
	}
	errText := s.errText
	if replayErr != "" && errText == "" {
		errText = replayErr
	}
	m := map[string]any{
		"available":         true,
		"state":             state,
		"error":             errText,
		"address":           address,
		"port":              s.listenPort,
		"menuProtocol":      1,
		"selectedVersion":   "26.50",
		"supportedVersions": []string{"26.50"},
		"signedIn":          s.tokenSource != nil,
		"accounts":          accounts,
		"authState":         authState,
		"authMessage":       s.auth.Message,
		"authLink":          s.auth.Link,
		"authCode":          s.auth.Code,
		"authError":         s.auth.Err,
		"sessionWarning":    warning,
		"accountName":       s.accountName,
		"players":           players,
		"replay":            replay,
		"assets":            assets,
	}
	b, _ := json.Marshal(m)
	return string(b)
}
