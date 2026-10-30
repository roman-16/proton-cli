package index

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "index",
		Short: "An encrypted, searchable copy of your mail, files and events on this machine",
	}
	c.AddCommand(listCmd(), createCmd(), updateCmd(), watchCmd(), deleteCmd())
	return c
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List what is indexed on this machine",
		Long: "List what is indexed on this machine.\n\n" +
			"Reads the files and nothing else, so it works signed out. INDEXED counts\n" +
			"what a search would look through; a build that has not finished says how\n" +
			"much of the app it has reached, and mail and drive say how many bodies and\n" +
			"file texts they hold while those are still downloading.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			indexes, err := c.App.Index.List()
			if err != nil {
				return refusal(err)
			}
			if err := kit.List(c, ui.TableSpec[search.Status]{
				Noun: "indexes",
				Columns: []ui.Column[search.Status]{
					{Header: "APP", Cell: func(s search.Status) string { return string(s.App) }},
					{Header: "INDEXED", Flex: true, Cell: indexedCell},
					{Header: "UNREADABLE", Cell: func(s search.Status) string { return blankAtZero(s.Unreadable) }},
					{Header: "UPDATED", Cell: func(s search.Status) string { return units.Time(s.Updated) }},
					{Header: "SIZE", Cell: func(s search.Status) string { return units.Size(s.Bytes) }},
				},
			}, indexes); err != nil {
				return err
			}
			if len(indexes) == 0 {
				c.UI().Hint("`" + kit.Program + " index create` makes your mail, files and events searchable on this machine.")
			}
			for _, index := range indexes {
				continues(c, index)
			}
			return nil
		}),
	}
}

func indexedCell(s search.Status) string {
	noun := ui.Quantity(s.Indexed, nounOf(s.App))
	switch {
	case !s.Complete && s.Total > s.Indexed:
		return fmt.Sprintf("%d of %s", s.Indexed, ui.Quantity(s.Total, nounOf(s.App)))
	case owesContents(s) && s.Texts != s.Indexed:
		return fmt.Sprintf("%s, %d of %s", noun, s.Bodies, ui.Quantity(s.Texts, owedOf(s.App)))
	case owesContents(s):
		return fmt.Sprintf("%s, %s", noun, ui.Quantity(s.Bodies, owedOf(s.App)))
	}
	return noun
}

func owesContents(s search.Status) bool { return s.Bodies < s.Texts }

func blankAtZero(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func continues(c *kit.Invocation, index search.Status) {
	if index.Complete && !index.Stale && !owesContents(index) {
		return
	}
	c.UI().Hint("`" + kit.Program + " index create " + string(index.App) + "` continues the download.")
}

func createCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create [REF...]",
		Short: "Index an app so its contents can be searched",
		Long: "Index an app so its contents can be searched.\n\n" +
			"Name the apps to index, or none for every app that can be. Mail and Drive\n" +
			"are indexed in two passes. Mail takes every message first, which takes\n" +
			"minutes and answers a filtered listing, then the bodies newest first, which\n" +
			"for a large mailbox takes hours. Drive takes the tree first, then the text of\n" +
			"your text files up to 1 MB each, most recently changed first; a PDF, an image\n" +
			"or anything larger is indexed by name alone. Stopping it and running it again\n" +
			"carries on where it left off.\n\n" +
			"What it writes is encrypted to your account's keys, under\n" +
			"~/.config/" + kit.Alias + "/index.",
		ValidArgsFunction: completeApps,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			apps, err := named(c.Args)
			if err != nil {
				return err
			}
			return bring(c, apps)
		}),
	}
}

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Bring every index up to date",
		Long: "Bring every index up to date.\n\n" +
			"Applies what has happened since the last run, carries on a build that was\n" +
			"interrupted, and reads an app again where Proton could not say what\n" +
			"changed. It creates nothing: an app with no index is left alone.\n\n" +
			"A search catches up by itself, so this is for having it done already:\n" +
			"run it from cron, or leave `index watch` attached.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			indexes, err := c.App.Index.List()
			if err != nil {
				return refusal(err)
			}
			apps := make([]search.App, 0, len(indexes))
			for _, index := range indexes {
				apps = append(apps, index.App)
			}
			return bring(c, apps)
		}),
	}
}

func bring(c *kit.Invocation, apps []search.App) error {
	parts, err := partsFor(c, apps)
	if err != nil {
		return err
	}
	if c.App.DryRun {
		return preview(c, parts)
	}
	lock, err := claim(c)
	if err != nil {
		return err
	}
	defer lock.Release()

	var done search.Result
	per := map[string]any{}
	for i, part := range parts {
		session, err := part.Open(c.Ctx)
		if err != nil {
			return refusal(err)
		}
		sink := ui.Batch(ui.NewCounter(c.UI(), part.Noun()), i+1, len(parts))
		got, err := run(c.Ctx, session, sink)
		done = search.Total(done, got)
		per[string(part.App())] = got.Indexed
		if err != nil {
			return stopped(c, session, part, err)
		}
		unreadable(c, part, got)
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Indexed, Kind: kind(parts), Count: done.Indexed + done.Removed,
		Detail: across(parts), Extra: per,
	}, func() error { return nil })
}

func run(ctx context.Context, session search.Session, sink progress.Sink) (search.Result, error) {
	var done search.Result
	for range reconciles {
		built, err := session.Build(ctx, sink)
		done = search.Total(done, built)
		if err != nil {
			return done, err
		}
		synced, err := session.Sync(ctx)
		done = search.Total(done, synced)
		if err != nil || !synced.Refreshed {
			return done, err
		}
	}
	return done, nil
}

const reconciles = 2

func stopped(c *kit.Invocation, session search.Session, part search.Part, err error) error {
	if !errors.Is(err, context.Canceled) {
		return err
	}
	status := session.Status()
	switch {
	case !status.Complete:
		c.Note("Indexed %d of %s so far. Run it again to continue.",
			status.Indexed, ui.Quantity(status.Total, part.Noun()))
	case owesContents(status):
		c.Note("Downloaded %d of %s so far. Run it again to continue.",
			status.Bodies, ui.Quantity(status.Texts, owedOf(part.App())))
	}
	return err
}

func unreadable(c *kit.Invocation, part search.Part, got search.Result) {
	if got.Unreadable == 0 {
		return
	}
	c.Warn("%s would not open; they are indexed by everything but their text.",
		ui.Quantity(got.Unreadable, contentsOf(part.App())))
}

func preview(c *kit.Invocation, parts []search.Part) error {
	total := 0
	for _, part := range parts {
		n, err := part.Count(c.Ctx)
		if err != nil {
			return err
		}
		total += n
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Indexed, Kind: kind(parts), Count: total, Detail: across(parts),
	}, func() error { return nil })
}

func watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Keep every index current until you stop it",
		Long: "Keep every index current until you stop it.\n\n" +
			"Applies changes as they land, one line per batch, and finishes a build\n" +
			"that was interrupted. It creates nothing: an app with no index is left\n" +
			"alone.\n\n" +
			"While it runs, a search reads the copy as of the last poll, at most 30\n" +
			"seconds old. Prefer it when searches are frequent; run `index update` on a\n" +
			"timer when they are not.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			indexes, err := c.App.Index.List()
			if err != nil {
				return refusal(err)
			}
			if len(indexes) == 0 {
				return kit.Fail("Nothing is indexed on this machine.").
					Hint(kit.Program + " index create")
			}
			apps := make([]search.App, 0, len(indexes))
			for _, index := range indexes {
				apps = append(apps, index.App)
			}
			parts, err := partsFor(c, apps)
			if err != nil {
				return err
			}
			lock, err := claim(c)
			if err != nil {
				return err
			}
			defer lock.Release()
			return kit.Watch(c, ui.StreamSpec[change]{
				Columns: []ui.StreamColumn[change]{
					{Width: 5, Cell: func(change) string { return time.Now().Format("15:04") }},
					{Width: 8, Cell: func(ch change) string { return string(ch.App) }},
					{Cell: func(ch change) string { return ch.What }},
				},
				Opening: "Watching " + ui.Listing(search.Names(apps)) + ", and indexing what changes. Ctrl+C to stop.",
			}, func(emit func(change) error) error {
				return follow(c, parts, emit)
			})
		}),
	}
}

type change struct {
	App  search.App `json:"app"`
	What string     `json:"change"`
}

const pollEvery = 30 * time.Second

func follow(c *kit.Invocation, parts []search.Part, emit func(change) error) error {
	sessions := make([]search.Session, 0, len(parts))
	for _, part := range parts {
		session, err := part.Open(c.Ctx)
		if err != nil {
			return refusal(err)
		}
		sessions = append(sessions, session)
	}
	for {
		select {
		case <-c.Ctx.Done():
			return nil
		case <-time.After(pollEvery):
		}
		for i, part := range parts {
			got, err := run(c.Ctx, sessions[i], progress.Nop{})
			switch {
			case c.Ctx.Err() != nil:
				return nil
			case err != nil:
				c.Warn("%s could not be brought up to date: %v", part.App(), err)
				continue
			case !got.Did():
				continue
			}
			if err := emit(change{App: part.App(), What: described(got, part.Noun())}); err != nil {
				return err
			}
		}
	}
}

func described(r search.Result, noun string) string {
	switch {
	case r.Indexed == 0:
		return fmt.Sprintf("%s removed", ui.Quantity(r.Removed, noun))
	case r.Removed == 0:
		return ui.Quantity(r.Indexed, noun) + " indexed"
	}
	return fmt.Sprintf("%s indexed, %d removed", ui.Quantity(r.Indexed, noun), r.Removed)
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "delete [REF...]",
		Annotations: map[string]string{kit.SignedOut: "yes"},
		Short:       "Remove an index from this machine",
		Long: "Remove an index from this machine.\n\n" +
			"Name the apps to remove, or none for all of them. Nothing in your account\n" +
			"changes, and searches go back to what Proton can answer.\n\n" +
			"Removing the last one removes the index key too; building again starts\n" +
			"from nothing.",
		ValidArgsFunction: completeApps,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			wanted, err := named(c.Args)
			if err != nil {
				return err
			}
			indexes, err := c.App.Index.List()
			if err != nil {
				return refusal(err)
			}
			var gone []search.Status
			for _, index := range indexes {
				for _, app := range wanted {
					if index.App == app {
						gone = append(gone, index)
					}
				}
			}
			if len(gone) == 0 {
				return kit.Fail("Nothing is indexed on this machine.").
					Hint(kit.Program + " index create").Exit(3)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "indexes", Count: len(gone),
				Name:   onlyApp(gone),
				Detail: "(" + units.Size(size(gone)) + ")",
			}, func() error {
				for _, index := range gone {
					if err := c.App.Index.Delete(index.App); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func onlyApp(gone []search.Status) string {
	if len(gone) != 1 {
		return ""
	}
	return string(gone[0].App)
}

func size(indexes []search.Status) int64 {
	var total int64
	for _, index := range indexes {
		total += index.Bytes
	}
	return total
}

func named(args []string) ([]search.App, error) {
	if len(args) == 0 {
		return search.Apps, nil
	}
	out := make([]search.App, 0, len(args))
	for _, arg := range args {
		app, ok := search.Known(arg)
		if !ok {
			return nil, kit.Fail("%q is not an app that can be indexed.", arg).
				Hint("one of: " + ui.Listing(search.Names(search.Apps)))
		}
		out = append(out, app)
	}
	return once(out), nil
}

func once(apps []search.App) []search.App {
	seen := make(map[search.App]bool, len(apps))
	out := make([]search.App, 0, len(apps))
	for _, app := range apps {
		if seen[app] {
			continue
		}
		seen[app] = true
		out = append(out, app)
	}
	return out
}

func partsFor(c *kit.Invocation, apps []search.App) ([]search.Part, error) {
	out := make([]search.Part, 0, len(apps))
	for _, app := range apps {
		switch app {
		case search.AppCalendar:
			out = append(out, c.App.Calendar.IndexPart())
		case search.AppDrive:
			out = append(out, c.App.Drive.IndexPart())
		case search.AppMail:
			out = append(out, c.App.Mail.IndexPart())
		default:
			return nil, kit.Fail("%q is not an app that can be indexed.", app)
		}
	}
	return out, nil
}

func completeApps(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return search.Names(search.Apps), cobra.ShellCompDirectiveNoFileComp
}

func claim(c *kit.Invocation) (*localfile.Lock, error) {
	lock, err := c.App.Index.Claim()
	if errors.Is(err, localfile.ErrBusy) {
		return nil, kit.Fail("Another run is already indexing.").
			Hint("wait for it to finish, or stop it").Exit(4)
	}
	if err != nil {
		return nil, refusal(err)
	}
	return lock, nil
}

func kind(parts []search.Part) string {
	if len(parts) == 1 {
		return parts[0].Noun()
	}
	return "entries"
}

func across(parts []search.Part) string {
	if len(parts) < 2 {
		return ""
	}
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		names = append(names, string(part.App()))
	}
	return "across " + ui.Listing(names)
}

func nounOf(app search.App) string {
	switch app {
	case search.AppCalendar:
		return "events"
	case search.AppDrive:
		return "items"
	case search.AppMail:
		return "messages"
	}
	return "entries"
}

func contentsOf(app search.App) string {
	switch app {
	case search.AppCalendar:
		return "events"
	case search.AppMail:
		return "message bodies"
	}
	return nounOf(app)
}

func owedOf(app search.App) string {
	if app == search.AppDrive {
		return "texts"
	}
	return "bodies"
}

func refusal(err error) error {
	switch {
	case errors.Is(err, search.ErrOtherAccount):
		return kit.Fail("The index belongs to another account.").
			Hint(kit.Program + " index delete").Exit(4)
	case errors.Is(err, search.ErrNotIndexed):
		return kit.Fail("Nothing is indexed on this machine.").
			Hint(kit.Program + " index create").Exit(3)
	}
	return err
}
