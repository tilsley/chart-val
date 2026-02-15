package domain

import "strings"

// ExtractChartNames parses file paths and returns unique chart names
// from paths matching {chartDir}/{name}/...
// This encapsulates the repository structure convention.
func ExtractChartNames(files []string, chartDir string) []string {
	prefix := chartDir + "/"
	seen := make(map[string]struct{})
	var names []string
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		// {chartDir}/{name}/... → extract {name}
		rest := f[len(prefix):]
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) < 1 || parts[0] == "" {
			continue
		}
		name := parts[0]
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}
