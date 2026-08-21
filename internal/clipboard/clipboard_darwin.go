package clipboard

// pbcopy ships with macOS, so there is nothing to probe for and nothing to fall back to.
func write(text string) error { return pipeTo(text, "pbcopy") }
