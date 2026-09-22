package room

import "github.com/Thanhphan1147/root-mn/pkg/root"

// Redact returns the per-viewer snapshot. The implementation lives in the engine
// so the server and the browser bot share exactly one redaction policy.
func Redact(g *root.Game, viewer string) map[string]any {
	return root.Redact(g, viewer)
}
