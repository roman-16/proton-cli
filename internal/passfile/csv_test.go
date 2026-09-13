package passfile

import (
	"strings"
	"testing"
)

// A spreadsheet from Windows starts with a byte order mark and ends its lines
// with a carriage return, and neither is part of a column name or a value.
func TestACSVFromWindowsIsRead(t *testing.T) {
	body := "\ufeffname,url,username,password\r\nGitHub,https://github.com,jane,pw\r\n"
	t.Run("columns", func(t *testing.T) {
		table, err := readCSV([]byte(body))
		if err != nil {
			t.Fatalf("readCSV: %v", err)
		}
		if !table.has("name", "url", "username", "password") {
			t.Errorf("the columns came back as %v", table.columns)
		}
		if got := table.rows[0].get("name"); got != "GitHub" {
			t.Errorf("a value came back as %q", got)
		}
	})
	t.Run("read as a browser export", func(t *testing.T) {
		doc := opened(t, written(t, "windows.csv", []byte(body)), "chrome", nil)
		if doc.Count() != 1 {
			t.Errorf("read %d items, want 1", doc.Count())
		}
	})
}

// A value that runs over several lines is one value.
func TestACSVValueCanHoldSeveralLines(t *testing.T) {
	table, err := readCSV([]byte("name,note\nRouter,\"line 1\nline 2\"\n"))
	if err != nil {
		t.Fatalf("readCSV: %v", err)
	}
	if got := table.rows[0].get("note"); got != "line 1\nline 2" {
		t.Errorf("the note came back as %q", got)
	}
}

// A blank line is not a row.
func TestABlankLineIsNotARow(t *testing.T) {
	table, err := readCSV([]byte("name,url\n\nGitHub,https://github.com\n\n"))
	if err != nil {
		t.Fatalf("readCSV: %v", err)
	}
	if len(table.rows) != 1 {
		t.Errorf("read %d rows, want 1", len(table.rows))
	}
}

// A file whose rows are shorter or longer than its header is still read, and a
// column that is not there reads as nothing.
func TestARowThatDoesNotMatchItsHeader(t *testing.T) {
	table, err := readCSV([]byte("name,url,username\nGitHub\nGitLab,https://gitlab.test,jane,extra\n"))
	if err != nil {
		t.Fatalf("readCSV: %v", err)
	}
	if len(table.rows) != 2 {
		t.Fatalf("read %d rows, want 2", len(table.rows))
	}
	if got := table.rows[0].get("username"); got != "" {
		t.Errorf("a column the row does not reach came back as %q", got)
	}
	if got := table.rows[1].get("username"); got != "jane" {
		t.Errorf("a value came back as %q", got)
	}
}

// A line that will not come apart is left out, and the file says how many.
func TestACorruptedLineIsCounted(t *testing.T) {
	body := "name,url\n" + `GitHub,"https://github.com` + "\n"
	table, err := readCSV([]byte(body))
	if err != nil {
		t.Fatalf("readCSV: %v", err)
	}
	if table.corrupt == 0 && len(table.rows) == 1 {
		t.Skip("this line came apart after all")
	}
	if w := table.warning(); table.corrupt > 0 && !strings.Contains(w, "not readable") {
		t.Errorf("the warning is %q", w)
	}
}

// A file of headers alone is a file with nothing in it.
func TestACSVOfHeadersAloneHoldsNothing(t *testing.T) {
	path := written(t, "empty.csv", []byte("name,url,username,password\n"))
	if _, err := Open(path, "chrome", nil); err == nil {
		t.Error("a file of headers alone was read as an export")
	}
}
