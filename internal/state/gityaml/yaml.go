package gityaml

import "gopkg.in/yaml.v3"

func yamlMarshal(v any) ([]byte, error) { return yaml.Marshal(v) }
