package render

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ResolveWithinRoot(root, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("path is required")
	}
	joined := filepath.Join(root, rel)
	cleanRoot := filepath.Clean(root)
	cleanJoined := filepath.Clean(joined)
	if cleanJoined != cleanRoot && !strings.HasPrefix(cleanJoined, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes checkout root: %q", rel)
	}
	return cleanJoined, nil
}
