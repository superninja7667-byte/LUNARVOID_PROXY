package engine

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/resource"
	"golang.org/x/oauth2"
)

const formID uint32 = 0x4C560001 // our menu form; real server forms use small ids

type runner struct {
	listener *minecraft.Listener
	status   *minecraft.ForeignStatusProvider
	mu       sync.Mutex
	conns    map[*minecraft.Conn]struct{}
}

func (r *runner) add(c *minecraft.Conn) {
	r.mu.Lock()
	r.conns[c] = struct{}{}
	r.mu.Unlock()
}

func (r *runner) remove(c *minecraft.Conn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}

func (r *runner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.conns)
}

func (r *runner) close() {
	_ = r.listener.Close()
	if r.status != nil {
		_ = r.status.Close()
	}
	r.mu.Lock()
	for c := range r.conns {
		_ = c.Close()
	}
	r.mu.Unlock()
}

func (s *engineState) fail(gen int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen {
		s.state, s.errText, s.note = "error", msg, ""
	}
}

func (s *engineState) start(host string, port int, lan bool) {
	host = strings.TrimSpace(host)
	s.mu.Lock()
	switch {
	case s.state == "starting" || s.state == "running":
		s.mu.Unlock()
		return
	case s.tokenSource == nil:
		s.state, s.errText = "error", "Sign in with your Microsoft account first."
		s.mu.Unlock()
		return
	case host == "" || port <= 0 || port > 65535:
		s.state, s.errText = "error", "Enter a server address and port."
		s.mu.Unlock()
		return
	}
	s.gen++
	gen := s.gen
	src := s.tokenSource
	s.runLan = lan
	s.state, s.errText, s.note = "starting", "", ""
	s.mu.Unlock()
	go s.runProxy(gen, host, port, src)
}

func (s *engineState) stop() {
	s.mu.Lock()
	s.gen++
	r := s.run
	s.run = nil
	s.state, s.errText, s.note = "stopped", "", ""
	s.mu.Unlock()
	if r != nil {
		r.close()
	}
}

// fetchServerPacks connects to the real server once and keeps the resource
// packs it sends. Minecraft needs those packs to draw the server's blocks, so
// the proxy has to pass them on. Downloaded packs are saved on the phone, so
// only the first connection is slow.
func (s *engineState) fetchServerPacks(gen int, remote string, src oauth2.TokenSource, cache *auth.XBLTokenCache, packCache minecraft.DirResourcePackCache) ([]*resource.Pack, error) {
	ctx, cancel := context.WithTimeout(auth.WithXBLTokenCache(context.Background(), cache), 8*time.Minute)
	defer cancel()
	go func() { // stop waiting if you press Stop
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !s.current(gen) {
					cancel()
					return
				}
			}
		}
	}()
	conn, err := dialWithSmallPackets(ctx, minecraft.Dialer{TokenSource: src, ResourcePackCache: packCache}, remote)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	packs := conn.ResourcePacks()
	out := make([]*resource.Pack, len(packs))
	copy(out, packs)
	return out, nil
}

func (s *engineState) runProxy(gen int, host string, port int, src oauth2.TokenSource) {
	defer func() {
		if rec := recover(); rec != nil {
			s.fail(gen, "Internal error: "+panicText(rec))
		}
	}()
	remote := net.JoinHostPort(host, strconv.Itoa(port))

	tok, err := src.Token()
	if err != nil {
		s.fail(gen, "Sign-in expired. Please sign in again.")
		return
	}
	s.mu.Lock()
	s.writeToken(tok)
	dir := s.dir
	listenPort := s.listenPort
	lan := s.runLan
	userPacks := s.packPathsLocked()
	cache := s.xblCache
	if cache == nil {
		cache = xblConfig.NewTokenCache()
		s.xblCache = cache
	}
	s.mu.Unlock()

	packPath, err := writePack(dir)
	if err != nil {
		s.fail(gen, "Could not prepare the menu pack: "+err.Error())
		return
	}
	menuPack, err := resource.ReadPath(packPath)
	if err != nil {
		s.fail(gen, "Could not load the menu pack: "+err.Error())
		return
	}
	packCache := minecraft.DirResourcePackCache{Dir: filepath.Join(dir, "packcache")}

	// Step 1: get the server's own resource packs so Minecraft can draw its blocks.
	s.setNote(gen, "Downloading the server's resources. The first time can take a few minutes.")
	serverPacks, perr := s.fetchServerPacks(gen, remote, src, cache, packCache)
	if !s.current(gen) {
		return
	}
	note := ""
	if perr != nil {
		note = "Could not download the server's resource pack, so some blocks may look invisible: " + perr.Error()
	}
	s.setNote(gen, note)

	// Step 2: open the proxy. Order: server packs, your own packs, then the menu on top.
	packs := append([]*resource.Pack{}, serverPacks...)
	for _, path := range userPacks {
		if extra, err := resource.ReadPath(path); err == nil {
			packs = append(packs, extra)
		}
	}
	packs = append(packs, menuPack)
	cfg := minecraft.ListenConfig{
		ResourcePacks:        packs,
		TexturePacksRequired: len(serverPacks) > 0,
	}
	var fsp *minecraft.ForeignStatusProvider
	if p, err := minecraft.NewForeignStatusProvider(remote); err == nil {
		cfg.StatusProvider = p
		fsp = p
	}
	// Only this phone can join unless "same Wi-Fi" mode is on.
	bind := "127.0.0.1:" + strconv.Itoa(listenPort)
	if lan {
		bind = ":" + strconv.Itoa(listenPort)
	}
	l, err := cfg.Listen("raknet", bind)
	if err != nil {
		if fsp != nil {
			_ = fsp.Close()
		}
		s.fail(gen, "Could not open port "+strconv.Itoa(listenPort)+": "+err.Error())
		return
	}

	r := &runner{listener: l, status: fsp, conns: map[*minecraft.Conn]struct{}{}}
	s.mu.Lock()
	if s.gen != gen {
		s.mu.Unlock()
		_ = l.Close()
		if fsp != nil {
			_ = fsp.Close()
		}
		return
	}
	s.run = r
	s.state = "running"
	s.mu.Unlock()

	for {
		c, err := l.Accept()
		if err != nil {
			break
		}
		go s.handle(r, c.(*minecraft.Conn), remote, src, cache, packCache)
	}

	s.mu.Lock()
	if s.gen == gen && s.state == "running" {
		s.state, s.run = "stopped", nil
	}
	s.mu.Unlock()
}

// session is one Minecraft client connected through the proxy.
type session struct {
	s      *engineState
	client *minecraft.Conn
	server *minecraft.Conn
	mu     sync.Mutex
	tab    string
	page   int
	cards  []card
}

func (s *engineState) handle(r *runner, conn *minecraft.Conn, remote string, src oauth2.TokenSource, cache *auth.XBLTokenCache, packCache minecraft.DirResourcePackCache) {
	r.add(conn)
	defer r.remove(conn)
	defer func() {
		if rec := recover(); rec != nil {
			msg := panicText(rec)
			s.noteError("Internal error: " + msg)
			_ = r.listener.Disconnect(conn, "LUNARVOID internal error: "+msg)
		}
	}()

	clientData := conn.ClientData()
	if path, slim, ok := s.skinPath(); ok {
		clientData = applySkin(clientData, path, slim)
	}
	// The shared token cache reuses the Xbox device token, and the pack cache
	// reuses packs already downloaded, so joining again is quick.
	ctx, cancel := context.WithTimeout(auth.WithXBLTokenCache(conn.Context(), cache), 3*time.Minute)
	server, err := dialWithSmallPackets(ctx, minecraft.Dialer{
		TokenSource:       src,
		ClientData:        clientData,
		ResourcePackCache: packCache,
	}, remote)
	cancel()
	if err != nil {
		_ = r.listener.Disconnect(conn, "LUNARVOID could not reach the server: "+err.Error())
		return
	}
	s.setAccountName(conn.IdentityData().DisplayName)

	sess := &session{s: s, client: conn, server: server, tab: menuTabs[0]}

	var wg sync.WaitGroup
	var startErr, spawnErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		startErr = conn.StartGame(server.GameData())
	}()
	go func() {
		defer wg.Done()
		spawnErr = server.DoSpawn()
	}()
	wg.Wait()
	if startErr != nil || spawnErr != nil {
		_ = server.Close()
		_ = r.listener.Disconnect(conn, "LUNARVOID could not start the game session.")
		return
	}

	var once sync.Once
	end := func(msg string) {
		once.Do(func() {
			_ = server.Close()
			_ = r.listener.Disconnect(conn, msg)
		})
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				end("LUNARVOID internal error: " + panicText(rec))
			}
		}()
		for {
			pk, err := server.ReadPacket()
			if err != nil {
				end(err.Error())
				return
			}
			if err := conn.WritePacket(pk); err != nil {
				end("connection lost")
				return
			}
		}
	}()

	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			end("connection lost")
			return
		}
		if sess.fromClient(pk) {
			continue
		}
		if err := server.WritePacket(pk); err != nil {
			end("connection lost")
			return
		}
	}
}

// fromClient looks at packets the player sends. It returns true when the
// packet was ours and must not be forwarded to the server.
func (c *session) fromClient(pk packet.Packet) bool {
	switch p := pk.(type) {
	case *packet.CommandRequest:
		line := strings.TrimSpace(p.CommandLine)
		if strings.HasPrefix(line, "/.") {
			c.command(strings.ToLower(strings.TrimSpace(line[2:])))
			return true
		}
	case *packet.ModalFormResponse:
		if p.FormID == formID {
			c.formResponse(p)
			return true
		}
	case *packet.Emote:
		if c.s.snapshot().EmoteMenu {
			c.openMenu(menuTabs[0], 0)
		}
	}
	return false
}

func (c *session) say(msg string) {
	_ = c.client.WritePacket(&packet.Text{TextType: packet.TextTypeRaw, Message: "§b[LUNARVOID]§r " + msg})
}

func (c *session) command(name string) {
	switch name {
	case "menu", "":
		c.openMenu(menuTabs[0], 0)
	case "help":
		c.say("/.menu opens the menu. Playing an emote opens it too.")
	case "replay":
		c.say("Replay is coming in the next build.")
	default:
		c.say("Unknown command. Try /.menu or /.help")
	}
}

func (c *session) openMenu(tab string, page int) {
	data, cards, shown := buildForm(tab, page, c.s.snapshot())
	c.mu.Lock()
	c.tab, c.page, c.cards = tab, shown, cards
	c.mu.Unlock()
	_ = c.client.WritePacket(&packet.ModalFormRequest{FormID: formID, FormData: data})
}

func (c *session) formResponse(p *packet.ModalFormResponse) {
	idx, ok := formIndex(p)
	if !ok {
		return // menu was closed
	}
	c.mu.Lock()
	cards, tab, page := c.cards, c.tab, c.page
	c.mu.Unlock()

	if idx >= 0 && idx < len(cards) {
		cd := cards[idx]
		switch cd.kind {
		case "mod":
			c.s.toggle(cd.arg)
		case "emote":
			c.s.toggle("emote")
		case "info":
			if cd.arg == "help" {
				c.say("/.menu opens the menu. Playing an emote opens it too.")
			} else {
				c.say("Add skins and resource packs in the LUNARVOID app (Player tab).")
			}
		}
		c.openMenu(tab, page)
		return
	}
	switch t := idx - len(cards); {
	case t >= 0 && t < len(menuTabs):
		c.openMenu(menuTabs[t], 0)
	case t == len(menuTabs):
		c.openMenu(tab, page-1)
	case t == len(menuTabs)+1:
		c.openMenu(tab, page+1)
	}
}

// formIndex reads which button was pressed. It works whether the library
// stores the answer as a plain byte slice or as an optional value.
func formIndex(p *packet.ModalFormResponse) (int, bool) {
	v := reflect.ValueOf(p).Elem().FieldByName("ResponseData")
	if !v.IsValid() {
		return 0, false
	}
	var data []byte
	if v.Kind() == reflect.Slice {
		data, _ = v.Interface().([]byte)
	} else if m := v.MethodByName("Value"); m.IsValid() {
		out := m.Call(nil)
		if len(out) == 2 && out[1].Kind() == reflect.Bool && out[1].Bool() {
			data, _ = out[0].Interface().([]byte)
		}
	}
	if len(data) == 0 {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return 0, false
	}
	return n, true
}
