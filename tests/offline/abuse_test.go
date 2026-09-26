package offline

import "testing"

// A report is judged from the command line before anything is sent: where it
// points, whether the reporter vouched for it, and whether the category has
// what Proton needs to act on it.

func TestAReportPointedAtYourOwnFilesIsRefused(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "abuse", "/invoice.exe", "--category", "malware", "--good-faith"},
		"Only something shared with you can be reported.", "--shared REF, or --link URL")
	refuses(t, 1, []string{"drive", "items", "revisions", "abuse", "/report.pdf", "5bH2mQxK",
		"--category", "malware", "--good-faith"},
		"Only something shared with you can be reported.", "--shared REF")
}

func TestAReportNeedsTheReportersWordForIt(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "abuse", "/invoice.exe", "--shared", "Project", "--category", "malware"},
		"--good-faith is required")
	refuses(t, 1, []string{"drive", "invitations", "abuse", "5bH2mQxK", "--category", "spam"},
		"--good-faith is required")
}

func TestAReportNamesWhatItIsAbout(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "abuse", "/invoice.exe", "--shared", "Project", "--good-faith"},
		`"category"`)
	refuses(t, 1, []string{"drive", "invitations", "abuse", "5bH2mQxK", "--category", "phishing", "--good-faith"},
		"--category accepts:", "spam", "copyright", "child-abuse", "non-consensual-intimate", "stolen-data", "malware", "other")
}

// A copyright or stolen-data report is one Proton answers, so it needs something
// to answer and somewhere to send the answer.
func TestACopyrightOrStolenDataReportNeedsAMessageAndAnAddress(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "abuse", "/", "--shared", "Project",
		"--category", "copyright", "--good-faith"},
		"A copyright report needs --message and --email.")
	refuses(t, 1, []string{"drive", "invitations", "abuse", "5bH2mQxK",
		"--category", "stolen-data", "--message", "Our customer records", "--good-faith"},
		"A stolen-data report needs --email.")
	refuses(t, 1, []string{"drive", "invitations", "abuse", "5bH2mQxK",
		"--category", "spam", "--email", "not-an-address", "--good-faith"},
		`"not-an-address" is not an email address.`)
}

// A computer is always your own, so a report offers no way into one.
func TestAReportCannotBePointedAtAComputer(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "abuse", "/invoice.exe", "--computer", "Work laptop",
		"--category", "malware", "--good-faith"}, "--computer")
}
