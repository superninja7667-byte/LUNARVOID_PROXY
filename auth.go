package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/auth"
	"golang.org/x/oauth2"
)

// All sign-in goes through the Android Minecraft title configuration (SISU),
// the same system the old LUNARVOID engine used.
var xblConfig = auth.AndroidConfig

var (
	linkRe = regexp.MustCompile(`https?://[^\s]+`)
	codeRe = regexp.MustCompile(`(?i)code\s+([A-Z0-9]{6,12})`)
	otcRe  = regexp.MustCompile(`otc=([A-Za-z0-9]+)`)
)

// captureWriter receives the "go to this page and enter this code" text that
// the sign-in library prints, so the app can show it.
type captureWriter struct {
	s   *engineState
	gen int
}

func (w *captureWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	if w.s.authGen != w.gen {
		return len(p), nil
	}
	w.s.auth.Message = text
	if m := linkRe.FindString(text); m != "" {
		w.s.auth.Link = strings.TrimRight(m, ".,")
	}
	if m := codeRe.FindStringSubmatch(text); m != nil {
		w.s.auth.Code = m[1]
	} else if m := otcRe.FindStringSubmatch(text); m != nil {
		w.s.auth.Code = m[1]
	}
	return len(p), nil
}

func (s *engineState) startSignIn() {
	s.mu.Lock()
	if s.auth.State == "pending" {
		s.mu.Unlock()
		return
	}
	s.authGen++
	gen := s.authGen
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	s.authCancel = cancel
	s.auth = authInfo{State: "pending"}
	s.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				s.mu.Lock()
				s.auth = authInfo{State: "error", Err: "Internal error: " + panicText(rec)}
				s.mu.Unlock()
			}
		}()
		tok, err := xblConfig.RequestLiveTokenContext(ctx, &captureWriter{s: s, gen: gen})
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.authGen != gen {
			return // cancelled
		}
		if err != nil {
			s.auth = authInfo{State: "error", Err: err.Error()}
			return
		}
		s.tokenSource = xblConfig.RefreshTokenSource(tok)
		s.xblCache = xblConfig.NewTokenCache()
		s.writeToken(tok)
		s.auth = authInfo{State: "done"}
		go s.refreshProfile() // show your gamertag right away
	}()
}

func (s *engineState) cancelSignIn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authGen++
	if s.authCancel != nil {
		s.authCancel()
		s.authCancel = nil
	}
	s.auth = authInfo{State: "idle"}
}

func (s *engineState) signOut() {
	s.stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authGen++
	if s.authCancel != nil {
		s.authCancel()
		s.authCancel = nil
	}
	s.tokenSource = nil
	s.xblCache = nil
	s.accountName = ""
	s.auth = authInfo{State: "idle"}
	if s.dir != "" {
		_ = os.Remove(filepath.Join(s.dir, "token.enc"))
		_ = os.Remove(filepath.Join(s.dir, "token.json"))
	}
	s.persist()
}

// writeToken saves the sign-in token, encrypted when the app gave us a key.
// The caller must hold s.mu.
func (s *engineState) writeToken(tok *oauth2.Token) {
	if s.dir == "" || tok == nil {
		return
	}
	b, err := json.Marshal(tok)
	if err != nil {
		return
	}
	if len(s.key) > 0 {
		if enc, err := s.seal(b); err == nil {
			_ = os.WriteFile(filepath.Join(s.dir, "token.enc"), enc, 0o600)
			_ = os.Remove(filepath.Join(s.dir, "token.json")) // old unencrypted file
			return
		}
	}
	_ = os.WriteFile(filepath.Join(s.dir, "token.json"), b, 0o600)
}

// readToken loads the saved token. The caller must hold s.mu.
func (s *engineState) readToken() *oauth2.Token {
	if s.dir == "" {
		return nil
	}
	var raw []byte
	if enc, err := os.ReadFile(filepath.Join(s.dir, "token.enc")); err == nil {
		if plain, err := s.open(enc); err == nil {
			raw = plain
		}
	}
	if raw == nil { // older build saved it unencrypted: read it, init() re-saves it encrypted
		if b, err := os.ReadFile(filepath.Join(s.dir, "token.json")); err == nil {
			raw = b
		}
	}
	if raw == nil {
		return nil
	}
	var t oauth2.Token
	if json.Unmarshal(raw, &t) != nil || t.RefreshToken == "" {
		return nil
	}
	return &t
}
