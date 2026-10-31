package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// applySkin swaps the player's skin in the login data the proxy sends to the
// server. If anything goes wrong it returns the original data untouched.
func applySkin(cd login.ClientData, path string, slim bool) login.ClientData {
	f, err := os.Open(path)
	if err != nil {
		return cd
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return cd
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if h*2 == w { // old 64x32 skins: pad to a square canvas
		h = w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, image.Rect(0, 0, b.Dx(), b.Dy()), img, b.Min, draw.Src)

	geometry, arm := "geometry.humanoid.custom", "wide"
	if slim {
		geometry, arm = "geometry.humanoid.customSlim", "slim"
	}
	patch, _ := json.Marshal(map[string]any{"geometry": map[string]string{"default": geometry}})
	sum := sha256.Sum256(dst.Pix)

	cd.SkinData = base64.StdEncoding.EncodeToString(dst.Pix)
	cd.SkinImageWidth, cd.SkinImageHeight = w, h
	cd.SkinID = fmt.Sprintf("lunarvoid-%x", sum[:6])
	cd.SkinResourcePatch = base64.StdEncoding.EncodeToString(patch)
	cd.SkinGeometry = ""
	cd.PersonaSkin = false
	cd.PremiumSkin = false
	cd.ArmSize = arm
	return cd
}
