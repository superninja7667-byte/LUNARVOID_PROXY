package engine

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// panicText turns a crash inside the engine into a short readable message
// (what went wrong and where), so the app can show it instead of closing.
func panicText(rec any) string {
	msg := fmt.Sprint(rec)
	lines := strings.Split(string(debug.Stack()), "\n")
	var frames []string
	for i := 0; i+1 < len(lines) && len(frames) < 3; i++ {
		l := lines[i+1]
		if strings.HasPrefix(l, "\t") && (strings.Contains(l, "gophertunnel") || strings.Contains(l, "/engine/") || strings.Contains(l, "go-raknet")) {
			f := strings.TrimSpace(l)
			if j := strings.LastIndex(f, "/"); j >= 0 {
				f = f[j+1:]
			}
			if k := strings.Index(f, " "); k >= 0 {
				f = f[:k]
			}
			frames = append(frames, f)
		}
	}
	if len(frames) > 0 {
		msg += " [" + strings.Join(frames, " < ") + "]"
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

func (s *engineState) noteError(msg string) {
	s.mu.Lock()
	s.errText = msg
	s.mu.Unlock()
}
