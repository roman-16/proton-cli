package calendar

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
)

// The public holidays calendars Proton offers, one per country and language.
// They are what `calendars create --holidays` takes; once added, one is a
// calendar like the others, under the same ID.

func holidaysCmd() *cobra.Command {
	c := &cobra.Command{Use: "holidays", Short: "Public holidays calendars you can add"}
	c.AddCommand(holidaysListCmd())
	return c
}

func holidaysColumns() []ui.Column[calsvc.Holidays] {
	return []ui.Column[calsvc.Holidays]{
		{Header: "ID", ID: true, Cell: func(h calsvc.Holidays) string { return h.ID }},
		{Header: "COUNTRY", Flex: true, Handle: true, Cell: func(h calsvc.Holidays) string { return h.Country }},
		{Header: "CODE", Cell: func(h calsvc.Holidays) string { return h.CountryCode }},
		{Header: "LANGUAGE", Cell: func(h calsvc.Holidays) string { return h.Language }},
		{Header: "ADDED", Cell: func(h calsvc.Holidays) string { return yesNo(h.Added) }},
	}
}

func holidaysListCmd() *cobra.Command {
	var held kit.Held[calsvc.Holidays]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the public holidays calendars you can add",
		Long: "List the public holidays calendars you can add.\n\n" +
			"These are what `calendars create --holidays` takes, by country name or code.\n" +
			"A country with holidays in more than one language has a row for each, and\n" +
			"ADDED says which of them you have.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Calendar.HolidaysDirectory(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[calsvc.Holidays]{
				Noun: "holidays calendars", Columns: holidaysColumns(),
			}, rows)
		}),
	}
	held.Register(c, "holidays calendars",
		kit.Key[calsvc.Holidays]{Name: "country", Less: func(a, b calsvc.Holidays) int {
			if n := kit.Fold(a.Country, b.Country); n != 0 {
				return n
			}
			return kit.Fold(a.Language, b.Language)
		}},
	)
	return c
}

// addHolidays adds the holidays calendar --holidays and --language name.
func addHolidays(c *kit.Invocation, country, language, color string) error {
	expanded, err := kit.Expand(c.App, country)
	if err != nil {
		return err
	}
	rows, err := c.App.Calendar.HolidaysDirectory(c.Ctx)
	if err != nil {
		return err
	}
	h, err := pickHolidays(rows, expanded, language)
	if err != nil {
		return err
	}
	if h.Added {
		return kit.Fail("You already have the holidays calendar for %s.", h.Country).Exit(4)
	}
	detail := ""
	if len(inCountry(rows, h.Country)) > 1 {
		detail = "in " + h.Language
	}
	return kit.Create(c, ui.ResultSpec{
		Action: ui.Added, Kind: "holidays calendars", Name: h.Country, Detail: detail,
	}, func() (string, error) {
		return c.App.Calendar.HolidaysAdd(c.Ctx, h.ID, color)
	})
}

// pickHolidays finds the holidays calendar a country and a language name.
//
// country is a country's name, its two-letter code, or the ID a listing showed.
// language is needed only where the country has holidays in more than one
// language, and is a language's name or its code.
func pickHolidays(rows []calsvc.Holidays, country, language string) (calsvc.Holidays, error) {
	var named []calsvc.Holidays
	for _, h := range rows {
		if h.ID == country || strings.EqualFold(h.Country, country) || strings.EqualFold(h.CountryCode, country) {
			named = append(named, h)
		}
	}
	if len(named) == 0 {
		return calsvc.Holidays{}, &errs.NotFound{
			Kind: "holidays calendar", Ref: country,
			Try: []string{kit.Program + " calendar settings holidays list"},
		}
	}
	matches := named
	if language != "" {
		matches = nil
		for _, h := range named {
			if strings.EqualFold(h.Language, language) || strings.EqualFold(h.LanguageCode, language) {
				matches = append(matches, h)
			}
		}
	}
	name := named[0].Country
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return calsvc.Holidays{}, kit.Fail("%s has no holidays calendar in %s; it has %s.",
			name, language, languages(named)).Exit(3)
	}
	return calsvc.Holidays{}, kit.Fail("%s has holidays in %s.", name, languages(matches)).
		Hint("--language " + matches[0].Language).Exit(4)
}

// inCountry is the holidays calendars one country has, one per language.
func inCountry(rows []calsvc.Holidays, country string) []calsvc.Holidays {
	var out []calsvc.Holidays
	for _, h := range rows {
		if strings.EqualFold(h.Country, country) {
			out = append(out, h)
		}
	}
	return out
}

func languages(rows []calsvc.Holidays) string {
	names := make([]string, 0, len(rows))
	for _, h := range rows {
		names = append(names, h.Language)
	}
	return ui.Listing(names)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
