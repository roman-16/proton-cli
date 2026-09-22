package kit

// opener names the launcher every macOS carries. It refuses when there is no
// window server to draw into, which is what a machine reached over SSH is.
func opener(url string) (string, []string) {
	return "open", []string{url}
}
