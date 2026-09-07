package migrate

import (
	"os"
	"path/filepath"
	"strings"
)

func join(parts ...string) string { return filepath.Join(parts...) }

func listYAML(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}
	return b, err
}
