package kit

// opener names the shell's own protocol handler rather than `cmd /c start`,
// which would need the address quoted for a shell that treats & as a separator -
// and a verification address carries one.
func opener(url string) (string, []string) {
	return "rundll32.exe", []string{"url.dll,FileProtocolHandler", url}
}
