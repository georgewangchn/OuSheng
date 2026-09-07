package model

// Assignment 是真正承重的关系：Actor × Role × System（v0.3 §10）。
// 区分谁执行（executor）与谁负责（accountable）。
type Assignment struct {
	Actor         string `yaml:"actor"`
	Role          string `yaml:"role"`
	System        string `yaml:"system"`
	Responsibility string `yaml:"responsibility"` // executor|accountable|reviewer|…
	Active        bool   `yaml:"active"`
}

type AssignmentsFile struct {
	SchemaVersion int         `yaml:"schema_version"`
	Assignments   []Assignment `yaml:"assignments"`
}

const (
	ResponsibilityExecutor    = "executor"
	ResponsibilityAccountable = "accountable"
)
