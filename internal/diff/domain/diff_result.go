package domain

// PRContext holds the details of a pull request event.
type PRContext struct {
	Owner          string
	Repo           string
	PRNumber       int
	BaseRef        string
	HeadRef        string
	HeadSHA        string
	InstallationID int64
}

// DiffResult represents the diff output for a single chart + environment pair.
type DiffResult struct {
	ChartName   string
	Environment string
	BaseRef     string
	HeadRef     string
	HasChanges  bool
	UnifiedDiff string
	Summary     string
}
