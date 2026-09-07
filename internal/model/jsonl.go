package model

import (
	"bytes"
	"encoding/json"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func splitLines(b []byte) [][]byte {
	return bytes.Split(b, []byte("\n"))
}
