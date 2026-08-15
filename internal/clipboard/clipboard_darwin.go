package clipboard

// pbcopy is macOS's own clipboard tool, part of the system since forever, so
// there is nothing to probe for and nothing to fall back to: if it is missing,
// something is wrong with the machine rather than with hop's guess about it.
func write(text string) error { return pipeTo(text, "pbcopy") }
