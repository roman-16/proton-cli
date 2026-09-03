// Package confirm decides which commands stop for a yes.
//
// The CLI has a built-in policy - it asks before something is removed for good,
// and before a filter removes things nobody named - and that policy is a floor
// nothing here can lower. What a person writes in their configuration sits on
// top of it: more commands may be made to ask, and some may be refused outright,
// but no directive makes proton less careful than it is with no configuration at
// all.
//
// That is why sources do not override one another the way ordinary settings do.
// Every other setting says how the CLI should behave for its user's convenience,
// so the most local answer is the right one. This one says what the CLI must not
// do by accident, so the most cautious answer is: a guard a nearer source can
// lower is not a guard.
package confirm

import (
	"fmt"
	"slices"
	"strings"
)

// Class names a set of commands by what they do, rather than by where they sit
// in the tree.
//
// Four of the five are read straight off the verb, which the command grammar has
// already declared and the conformance test already checks. Nothing is annotated
// per command, so a command written next year is classified the moment somebody
// picks its verb.
type Class int

const (
	// Default asks for nothing beyond the built-in policy. It is how a narrower
	// scope opts out of a broader directive: "every change asks, but Drive keeps
	// the usual rules".
	Default Class = iota
	// Deletions covers the verbs whose work nothing can take back.
	Deletions
	// Mutations covers every verb that changes state.
	Mutations
	// Reads covers every verb that does not.
	Reads
	// All covers every command there is.
	All
)

// Classes is the vocabulary, in the order the documentation lists it.
var Classes = []Class{Default, Deletions, Mutations, Reads, All}

var classNames = map[Class]string{
	Default:   "default",
	Deletions: "deletions",
	Mutations: "mutations",
	Reads:     "reads",
	All:       "all",
}

func (c Class) String() string { return classNames[c] }

// Subject names what a class covers, as the subject of a sentence about it.
//
// All has none: a policy that covers every command has nothing narrower to say
// than the command that was actually run, so the caller names that instead.
func (c Class) Subject() string {
	switch c {
	case Deletions:
		return "Deleting"
	case Mutations:
		return "Changing anything"
	case Reads:
		return "Reading"
	}
	return ""
}

// ParseClass reads a class by name.
func ParseClass(s string) (Class, error) {
	for class, name := range classNames {
		if name == s {
			return class, nil
		}
	}
	return 0, fmt.Errorf("%q is not one of %s", s, ClassList())
}

// ClassList names every class, for an error that has to offer the choice.
func ClassList() string {
	names := make([]string, 0, len(Classes))
	for _, c := range Classes {
		names = append(names, c.String())
	}
	return strings.Join(names, ", ")
}

// Outcome is what the policy demands of one command.
type Outcome int

const (
	// Allow asks for nothing. The built-in policy still applies.
	Allow Outcome = iota
	// Ask stops for a yes, which --yes answers in advance.
	Ask
	// Deny refuses. Nothing answers it: not --yes, not --dry-run, and not a
	// directive from a nearer source. Lifting it means editing the file that
	// declared it, which is the whole of its value.
	Deny
)

// Command is what the policy is asked about: where it sits, and the two things
// its verb already says about it.
type Command struct {
	// Path is the command without the program name: {"mail", "messages", "delete"}.
	Path []string
	// Mutating says the command changes state.
	Mutating bool
	// Irreversible says neither this CLI nor Proton's own clients can undo it.
	Irreversible bool
}

// covers reports whether a class takes in this command.
func (c Class) covers(cmd Command) bool {
	switch c {
	case Deletions:
		return cmd.Irreversible
	case Mutations:
		return cmd.Mutating
	case Reads:
		return !cmd.Mutating
	case All:
		return true
	}
	return false
}

// Directive is one line of policy: which commands, which class of them, and
// what happens when one is run.
type Directive struct {
	// Path is the scope, as command words without the program name. Empty is
	// every command, which is what "*" spells.
	Path    []string
	Class   Class
	Outcome Outcome
}

// scopes reports whether this directive's path contains the command.
func (d Directive) scopes(cmd Command) bool {
	if len(d.Path) > len(cmd.Path) {
		return false
	}
	return slices.Equal(d.Path, cmd.Path[:len(d.Path)])
}

// Source is the directives one place declared: a section of the configuration
// file, an environment variable, a flag.
//
// Scope only carries within a source. A directive says what it says about the
// commands under it and nothing about how loudly some other source spoke, so a
// narrow `default` written in one place cannot quietly stand down a broad rule
// written in another.
type Source []Directive

// Policy is every source in force.
//
// They do not override one another. Each answers for the command on its own,
// and the most cautious of those answers is the one that holds - which is what
// makes a policy ratchet: adding a source can tighten it and can never loosen
// it, whichever order they arrive in.
type Policy []Source

// Decision is what the policy demands of a command, and the class that decided
// it - which is what lets a refusal say why rather than only that.
type Decision struct {
	Outcome Outcome
	Class   Class
}

// Require says what the policy demands of a command.
func (p Policy) Require(cmd Command) Decision {
	strongest := Decision{Outcome: Allow}
	for _, source := range p {
		if d := source.require(cmd); d.Outcome > strongest.Outcome {
			strongest = d
		}
	}
	return strongest
}

// require is one source's answer.
//
// Deny is settled first and on its own, so a narrower ask can never stand in
// front of a broader refusal. Within each outcome the narrowest scope wins,
// which is what lets one directive carve an exception out of another: an `ask`
// of `mutations` everywhere and `default` under Drive leaves Drive alone.
func (s Source) require(cmd Command) Decision {
	for _, outcome := range []Outcome{Deny, Ask} {
		if class, ok := s.settles(cmd, outcome); ok {
			return Decision{Outcome: outcome, Class: class}
		}
	}
	return Decision{Outcome: Allow}
}

// settles reports whether the directives of one outcome demand it of this
// command, and which class said so. Only the narrowest scope that mentions the
// command is consulted.
func (s Source) settles(cmd Command, outcome Outcome) (Class, bool) {
	depth, found, class := -1, false, Default
	for _, d := range s {
		if d.Outcome != outcome || !d.scopes(cmd) {
			continue
		}
		if len(d.Path) > depth {
			depth, found = len(d.Path), false
		}
		if len(d.Path) == depth && !found && d.Class.covers(cmd) {
			found, class = true, d.Class
		}
	}
	return class, found
}

// Describe writes the policy back out in the one-line form it can be read from,
// so a report can state which commands this machine stops for.
//
// An unstated policy is "default", which is the word for it in every place a
// policy can be written, rather than an empty string that reads as a fact
// nobody established.
func (p Policy) Describe() string {
	var parts []string
	for _, source := range p {
		for _, d := range source {
			if len(d.Path) == 0 {
				parts = append(parts, d.Class.String())
				continue
			}
			parts = append(parts, d.Class.String()+":"+strings.Join(d.Path, " "))
		}
	}
	if len(parts) == 0 {
		return Default.String()
	}
	return strings.Join(parts, ",")
}

// Paths is every scope the policy names, so a caller holding the command tree
// can check that each one is a command.
func (p Policy) Paths() [][]string {
	var out [][]string
	for _, source := range p {
		for _, d := range source {
			if len(d.Path) > 0 {
				out = append(out, d.Path)
			}
		}
	}
	return out
}
