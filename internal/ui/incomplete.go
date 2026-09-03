package ui

// IncompleteSpec is what an answer could not include.
//
// Items decrypt on this machine, one at a time, and one that will not open is no
// reason to refuse the other forty-one. But a listing that quietly drops it is
// a wrong answer presented as a right one: the count agrees with the rows, the
// exit code says success, and nothing on the screen suggests that the thing
// somebody is looking for was there all along and could not be read.
type IncompleteSpec struct {
	// Count is how many things could not be shown.
	Count int
	// Kind is the singular noun for what they were: "item", "vault", "folder".
	Kind string
	// Hides marks a container, whose loss takes its contents with it. How many
	// of those there were is exactly what could not be read, so the sentence
	// says that instead of counting.
	Hides bool
	// Remedy is what to do about it, phrased by the layer that knows what this
	// program is called.
	Remedy string
}

// Incomplete says that what was just printed is short, and how to find out why.
//
// One line with a count rather than one line per thing: a vault that will not
// open would otherwise bury the answer it was attached to under five hundred
// identical warnings.
func (u *UI) Incomplete(spec IncompleteSpec) {
	if spec.Count == 0 || spec.Kind == "" {
		return
	}
	msg := spec.Sentence()
	if spec.Remedy != "" {
		msg += "\n" + spec.Remedy
	}
	u.Warn(msg)
}

// Sentence is the wording, which depends on whether what went missing took
// anything with it.
func (spec IncompleteSpec) Sentence() string {
	subject := Quantity(spec.Count, spec.Kind+"s")
	switch {
	case spec.Hides && spec.Count == 1:
		return subject + " could not be opened, so nothing inside it is listed."
	case spec.Hides:
		return subject + " could not be opened, so nothing inside them is listed."
	case spec.Count == 1:
		return subject + " could not be decrypted and is not listed."
	}
	return subject + " could not be decrypted and are not listed."
}
