package domain

// Chart represents a Helm chart at a specific git ref.
type Chart struct {
	Name             string
	Version          string
	Path             string // path within the repository
	RenderedManifest []byte
}

// ValuesFile represents a Helm values file.
type ValuesFile struct {
	Path    string
	Content []byte
}
