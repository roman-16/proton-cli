package cli

import (
	"errors"
	"strings"

	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// resolveSettings reads the configuration file and settles every setting for
// this invocation.
//
// A policy scope is checked against the tree here rather than taken on trust,
// because a scope that names no command silently guards nothing - and a guard
// that is quietly absent is worse than one that was never written.
//
// It always answers with settings a run can proceed on, and separately with what
// stopped them being the ones asked for. Every command but one stops there. The
// exception is `report`, which exists to explain a machine that is not working
// and so cannot be stopped by one - it runs on the defaults and carries the
// reason, which is the news.
func resolveSettings(root *cobra.Command, flags config.Flags) (config.Resolved, error) {
	path, from, err := config.Path(flags.Config)
	if err != nil {
		return defaults(from, err), errs.Problemf("Could not find the configuration directory: %v.", err)
	}
	file, from, err := config.Load(path, from)
	if err != nil {
		return defaults(from, err), errs.Problemf("%v", err)
	}
	settings, err := config.Resolve(file, flags)
	if err != nil {
		return defaults(from, err), present(err)
	}
	settings.Source = from
	if err := checkScopes(root, settings.Confirm); err != nil {
		return defaults(from, err), err
	}
	return settings, nil
}

// defaults is what a run gets when the settings will not settle, carrying why.
func defaults(from config.Source, why error) config.Resolved {
	settings := config.Defaults()
	settings.Source = from.Ignoring(why)
	return settings
}

// present leaves an error that already knows how to say itself alone, and makes
// a sentence of one that does not.
func present(err error) error {
	var problem *errs.Problem
	if errors.As(err, &problem) {
		return problem
	}
	return errs.Problemf("%v.", err)
}

// checkScopes rejects a confirmation scope that names no command.
func checkScopes(root *cobra.Command, policy confirm.Policy) error {
	for _, scope := range policy.Paths() {
		found, _, err := root.Find(scope)
		if err != nil || found == root || !strings.HasSuffix(found.CommandPath(), strings.Join(scope, " ")) {
			problem := errs.Problemf("%q is not a command, so it cannot be a confirmation scope.",
				strings.Join(scope, " "))
			if len(scope) > 0 {
				for _, s := range root.SuggestionsFor(scope[0]) {
					problem = problem.Hint(s)
				}
			}
			return problem
		}
	}
	return nil
}
