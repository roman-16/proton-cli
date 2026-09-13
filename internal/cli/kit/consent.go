package kit

import (
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// The confirmation policy reaches a command in two places, because two different
// things are known in each.
//
// Before any step runs, the command's own name is all there is - which is enough
// to refuse it outright, and enough to stop a command that reads. Nothing about
// a listing improves by resolving it first, and a read never reaches the one
// place a mutation is stopped.
//
// A mutation waits. By the time it reaches Mutate the filter has run and the
// question can name what it would touch, so asking early would trade a sentence
// that counts for one that guesses.

// ExitRefused is what a command denied by the policy exits with. It is its own
// code because a caller has to be able to tell a refusal from a mistake.
const ExitRefused = 6

// classify describes a command to the policy.
//
// Both facts come from the verb, which the grammar has already declared and the
// conformance test already checks against the actions. Nothing is annotated per
// command, so a command added next year is classified the moment somebody picks
// its verb.
func classify(cmd *cobra.Command) confirm.Command {
	if cmd == nil {
		return confirm.Command{}
	}
	verb := cmd.Name()
	return confirm.Command{
		Path:         strings.Fields(cmd.CommandPath())[1:],
		Mutating:     Mutating[verb],
		Irreversible: Irreversible[verb],
	}
}

// gate applies the policy before a command does anything at all.
func gate(c *Invocation) error {
	cmd := classify(c.Cmd)
	decision := c.App.Confirm.Require(cmd)
	switch decision.Outcome {
	case confirm.Deny:
		return refused(c.Cmd, decision.Class)
	case confirm.Ask:
		if cmd.Mutating && !Opaque[c.Cmd.Name()] {
			return nil
		}
		return askToRun(c, decision.Class)
	}
	return nil
}

// refused is what a denied command answers.
//
// It carries no remedy, and that is the point: the reader is not stuck, they are
// stopped, and the one thing this sentence must not do is hand whatever ran the
// command the edit that would remove the guard.
func refused(cmd *cobra.Command, class confirm.Class) error {
	subject := class.Subject()
	if subject == "" {
		subject = "running " + named(cmd)
	}
	return errs.Problemf("%s is turned off by your confirmation policy.", subject).Exit(ExitRefused)
}

// askToRun stops a command whose whole description is the line that was typed,
// naming it back, because there is nothing else true to say about it.
func askToRun(c *Invocation, class confirm.Class) error {
	if c.App.Yes {
		return nil
	}
	if !c.UI().CanPrompt() {
		return Fail("running %s needs confirmation. %s", invoked(c), policyAsks(class)).
			Hint("--yes to confirm.")
	}
	ok, err := c.UI().Confirm(fmt.Sprintf("Would run %s. Continue?", invoked(c)))
	if err != nil {
		return err
	}
	if !ok {
		return Fail("Cancelled.")
	}
	return nil
}

// invoked is the command as it was typed, with the arguments of one whose effect
// they decide.
//
// `proton api` names nothing a person can answer about. `proton api DELETE
// /pass/v1/vault/{id}` is the entire question.
func invoked(c *Invocation) string {
	name := named(c.Cmd)
	if c.Cmd == nil || !Opaque[c.Cmd.Name()] {
		return name
	}
	return strings.TrimSpace(name + " " + strings.Join(c.Args, " "))
}

// policyAsks says why a command nobody would otherwise be asked about is asking.
func policyAsks(class confirm.Class) string {
	return fmt.Sprintf("Your confirmation policy asks about %s.", class)
}

// named is the whole invocation, program included, because a refusal may be read
// far from the terminal that produced it.
//
// Every sentence it appears in puts something in front of it. A command path is
// lower case and a message is a sentence, so one that opened with the path would
// have it capitalised - and `Proton mail messages list` reads as the company
// rather than as the command that was run.
func named(cmd *cobra.Command) string {
	if cmd == nil {
		return Program
	}
	return cmd.CommandPath()
}
