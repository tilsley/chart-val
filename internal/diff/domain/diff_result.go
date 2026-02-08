package domain

// Status represents the outcome of a diff operation.
type Status int

const (
	StatusSuccess Status = iota // No changes detected
	StatusChanges                // Changes detected
	StatusError                  // Error occurred during diff
)

// String returns the string representation of the Status.
// Implements the Stringer interface.
func (s Status) String() string {
	if s < 0 || int(s) >= len(statusNames) {
		return "Unknown"
	}
	return statusNames[s]
}

var statusNames = [...]string{
	StatusSuccess: "Success",
	StatusChanges: "Changes",
	StatusError:   "Error",
}

// DiffResult represents the diff output for a single chart + environment pair.
type DiffResult struct {
	ChartName    string
	Environment  string
	BaseRef      string
	HeadRef      string
	Status       Status // Outcome of the diff operation
	UnifiedDiff  string // Traditional line-based diff (go-difflib)
	SemanticDiff string // Semantic YAML diff (dyff) - may be empty if dyff unavailable
	Summary      string // Human-readable summary (or error message if Status == StatusError)
}

// PreferredDiff returns the semantic diff if available, otherwise the unified diff.
// This allows reporting adapters to prefer semantic diffs while falling back to unified.
func (r DiffResult) PreferredDiff() string {
	if r.SemanticDiff != "" {
		return r.SemanticDiff
	}
	return r.UnifiedDiff
}

// CountByStatus returns counts of results grouped by status.
func CountByStatus(results []DiffResult) (success, changes, errors int) {
	for _, r := range results {
		switch r.Status {
		case StatusSuccess:
			success++
		case StatusChanges:
			changes++
		case StatusError:
			errors++
		}
	}
	return
}

// DiffLabel creates an identifier for a diff comparison.
// Example: "my-app/prod (main)"
func DiffLabel(chartName, envName, ref string) string {
	return chartName + "/" + envName + " (" + ref + ")"
}

// GroupByChart groups results by ChartName, preserving insertion order.
// Returns a slice of slices, where each inner slice contains all results
// for a single chart.
func GroupByChart(results []DiffResult) [][]DiffResult {
	order := make(map[string]int)
	var groups [][]DiffResult

	for _, r := range results {
		idx, exists := order[r.ChartName]
		if !exists {
			idx = len(groups)
			order[r.ChartName] = idx
			groups = append(groups, nil)
		}
		groups[idx] = append(groups[idx], r)
	}
	return groups
}
