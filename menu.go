package engine

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:pack
var packFiles embed.FS

// Left-hand tabs of the in-game menu, in order.
var menuTabs = []string{"HUD", "Combat", "Movement", "Visuals", "World", "Client"}

// Every form ends with the tab buttons and two page buttons (previous, next).
// The menu pack counts on exactly this many (see "#form_button_length - 8"
// in pack/ui/lunarvoid.json), so keep these two numbers in step.
const tailButtons = 8

const perPage = 9

type card struct {
	name, desc, kind, arg string // kind: mod, emote, info
}

func modulesFor(tab string) []card {
	if tab == "Client" {
		return []card{
			{"Emote menu", "Open this menu by playing an emote.", "emote", ""},
			{"Skins & packs", "Manage them in the LUNARVOID app.", "info", "skins"},
			{"Help", "Commands and tips.", "info", "help"},
		}
	}
	var out []card
	for _, name := range catalog[tab] {
		d := catalogDesc[name]
		if d == "" {
			d = "Preview only."
		}
		out = append(out, card{name, d, "mod", name})
	}
	return out
}

func tabNote(tab string) string {
	switch tab {
	case "Client":
		return "Menu and app shortcuts"
	case "World":
		return "No modules here yet"
	}
	return "Preview only: switches do nothing yet"
}

func isOn(c card, st settings) bool {
	switch c.kind {
	case "emote":
		return st.EmoteMenu
	case "mod":
		return st.Mods[c.arg]
	}
	return false
}

// buildForm makes the JSON for a server form. Buttons are the cards first,
// then the tab buttons, then previous/next. Text prefixes tell the menu pack
// how to draw them: "on:" = switched on, "tab:"/"tabon:" = sidebar tab
// (inactive/active), "nav:"/"navoff:" = page button (usable/greyed out).
func buildForm(tab string, page int, st settings) (body []byte, cards []card, shownPage int) {
	known := false
	for _, t := range menuTabs {
		if t == tab {
			known = true
		}
	}
	if !known {
		tab = menuTabs[0]
	}
	all := modulesFor(tab)
	pages := (len(all) + perPage - 1) / perPage
	if pages == 0 {
		pages = 1
	}
	if page >= pages {
		page = pages - 1
	}
	if page < 0 {
		page = 0
	}
	start := page * perPage
	end := start + perPage
	if end > len(all) {
		end = len(all)
	}
	cards = all[start:end]

	buttons := make([]map[string]string, 0, len(cards)+tailButtons)
	for _, c := range cards {
		prefix, line2 := "", "§7"+shorten(c.desc, 24)
		if c.kind != "info" {
			if isOn(c, st) {
				prefix, line2 = "on:", "§aON"
			} else {
				line2 = "§cOFF"
			}
		}
		buttons = append(buttons, map[string]string{"text": prefix + "§l" + c.name + "§r\n" + line2})
	}
	for _, t := range menuTabs {
		p := "tab:"
		if t == tab {
			p = "tabon:"
		}
		buttons = append(buttons, map[string]string{"text": p + t})
	}
	prev, next := "navoff:", "navoff:"
	if page > 0 {
		prev = "nav:"
	}
	if page < pages-1 {
		next = "nav:"
	}
	buttons = append(buttons, map[string]string{"text": prev + "< Prev"}, map[string]string{"text": next + "Next >"})

	content := "§l" + tab + "§r\n§7" + tabNote(tab)
	if pages > 1 {
		content += fmt.Sprintf("  ·  page %d of %d", page+1, pages)
	}
	body, _ = json.Marshal(map[string]any{
		"type":    "form",
		"title":   "LUNARVOID",
		"content": content,
		"buttons": buttons,
	})
	return body, cards, page
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// writePack zips the menu resource pack into dir and returns its path.
func writePack(dir string) (string, error) {
	path := filepath.Join(dir, "lunarvoid-menu.mcpack")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	add := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	err = fs.WalkDir(packFiles, "pack", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := packFiles.ReadFile(p)
		if err != nil {
			return err
		}
		return add(strings.TrimPrefix(p, "pack/"), data)
	})
	if err != nil {
		return "", err
	}
	if err := add("textures/lunarvoid/pixel.png", pixelPNG()); err != nil {
		return "", err
	}
	if err := add("textures/lunarvoid/logo.png", logoPNG()); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func pixelPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// logoPNG draws the LUNARVOID mark: the same four-point shape as the app logo.
func logoPNG() []byte {
	const n = 64
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	poly := [][2]float64{{0, 0}, {40, 7.2}, {22, 40}, {12, 23.2}}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			px, py := (float64(x)+0.5)*40/n, (float64(y)+0.5)*40/n
			if inside(poly, px, py) {
				t := (float64(x) + float64(y)) / (2 * n)
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(97 - 89*t), G: uint8(232 - 113*t), B: 255, A: 255})
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func inside(poly [][2]float64, x, y float64) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		xi, yi, xj, yj := poly[i][0], poly[i][1], poly[j][0], poly[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}
