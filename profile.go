package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/auth"
)

// refreshProfile looks up your Xbox gamertag so the app can show it as soon as
// you are signed in (and every time the app starts). If anything fails the app
// just keeps showing "Microsoft account"; it never blocks sign-in.
func (s *engineState) refreshProfile() {
	defer func() { _ = recover() }() // the gamertag is a nice-to-have: never crash for it
	s.mu.Lock()
	src := s.tokenSource
	cache := s.xblCache
	s.mu.Unlock()
	if src == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if cache != nil {
		ctx = auth.WithXBLTokenCache(ctx, cache)
	}

	live, err := src.Token()
	if err != nil {
		return
	}
	xbl, err := xblConfig.RequestXBLToken(ctx, live, "http://xboxlive.com")
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://profile.xboxlive.com/users/me/profile/settings?settings=Gamertag", nil)
	if err != nil {
		return
	}
	xbl.SetAuthHeader(req)
	req.Header.Set("x-xbl-contract-version", "3")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		ProfileUsers []struct {
			Settings []struct {
				ID    string `json:"id"`
				Value string `json:"value"`
			} `json:"settings"`
		} `json:"profileUsers"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return
	}
	for _, u := range body.ProfileUsers {
		for _, st := range u.Settings {
			if st.ID == "Gamertag" && st.Value != "" {
				s.setAccountName(st.Value)
				return
			}
		}
	}
}
