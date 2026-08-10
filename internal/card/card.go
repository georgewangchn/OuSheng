package card

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

type Status string

const (
	Proposed   Status = "proposed"
	Agreed     Status = "agreed"
	Live       Status = "live"
	Verified   Status = "verified"
	Deprecated Status = "deprecated"
)

type Contract struct {
	Kind      string `yaml:"kind"`
	Breaking  bool   `yaml:"breaking"`
	Interface any    `yaml:"interface"`
}

type Evidence struct {
	Probe          string `yaml:"probe"`
	PassedAtCommit string `yaml:"passed_at_commit"`
	By             string `yaml:"by"`
}

type HumanAck struct {
	Approver  string `yaml:"approver"`
	AtVersion int    `yaml:"at_version"`
}

type Card struct {
	ID        string    `yaml:"id"`
	Owner     string    `yaml:"owner"`
	Task      string    `yaml:"task"`
	Status    Status    `yaml:"status"`
	Version   int       `yaml:"version"`
	DependsOn []string  `yaml:"depends_on,omitempty"`
	Contract  Contract  `yaml:"contract"`
	Evidence  *Evidence `yaml:"evidence,omitempty"`
	HumanAck  *HumanAck `yaml:"human_ack,omitempty"`
}

type Summary struct {
	ID        string   `json:"id"`
	Owner     string   `json:"owner"`
	Task      string   `json:"task"`
	Status    string   `json:"status"`
	Version   int      `json:"version"`
	DependsOn []string `json:"depends_on,omitempty"`
}

func (c Card) Summarize() Summary {
	return Summary{
		ID:        c.ID,
		Owner:     c.Owner,
		Task:      c.Task,
		Status:    string(c.Status),
		Version:   c.Version,
		DependsOn: c.DependsOn,
	}
}

func Decode(b []byte) (Card, error) {
	var c Card
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Card{}, err
	}
	return c, nil
}

func Encode(c Card) ([]byte, error) {
	return yaml.Marshal(c)
}
