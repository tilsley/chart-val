// Package linediff provides simple line-by-line diff computation.
package linediff

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// Adapter implements ports.DiffPort using traditional line-by-line unified diff.
type Adapter struct{}

// New creates a new line-based diff adapter.
func New() *Adapter {
	return &Adapter{}
}

// ComputeDiff performs traditional line-by-line unified diff.
func (a *Adapter) ComputeDiff(baseName, headName string, base, head []byte) string {
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(base)),
		B:        difflib.SplitLines(string(head)),
		FromFile: baseName,
		ToFile:   headName,
		Context:  3, // Show 3 lines of context around changes
	}
	text, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return fmt.Sprintf("error computing diff: %s", err)
	}
	return strings.TrimSpace(text)
}
