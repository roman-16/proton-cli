// Package argv finds a command inside an argument list.
//
// The suite's runners wrap what a test asked for: consent goes in front, an
// output format goes in front, a credential file goes after the command's own
// words. So anything that has to recognise the command being run - to refuse
// it, to hand it a password, or to notice that it will make Proton write to
// somebody - has to look for those words rather than compare the whole list.
package argv

import "slices"

// At reports where args holds these words in order and adjacent, or -1.
func At(args []string, words ...string) int {
	for i := 0; i+len(words) <= len(args); i++ {
		if slices.Equal(args[i:i+len(words)], words) {
			return i
		}
	}
	return -1
}

// Has reports whether args holds these words in order and adjacent, wherever
// the wrapping put them.
func Has(args []string, words ...string) bool { return At(args, words...) >= 0 }
