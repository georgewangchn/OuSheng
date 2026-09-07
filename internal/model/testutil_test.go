package model

import (
	"os"
)

type sliceWriter struct{ b *[]byte }

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}

func writeFile(path string, b []byte) error { return os.WriteFile(path, b, 0o644) }
