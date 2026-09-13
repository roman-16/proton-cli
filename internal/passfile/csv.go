package passfile

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Most managers export a CSV with a header row, and differ only in what they
// call the columns. Reading one by name rather than by position is what lets a
// reader be the list of column names it cares about.

type table struct {
	columns []string
	index   map[string]int
	rows    []row
	// corrupt is how many lines would not come apart, which is worth saying
	// once rather than per line.
	corrupt int
}

type row struct {
	cells []string
	index map[string]int
}

func readCSV(data []byte) (*table, error) {
	r := csv.NewReader(bytes.NewReader(stripBOM(data)))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.ReuseRecord = false

	header, err := r.Read()
	if err != nil {
		return nil, errors.New("it holds no rows")
	}
	t := &table{index: map[string]int{}}
	for i, name := range header {
		name = strings.TrimSpace(strings.Trim(name, "\""))
		t.columns = append(t.columns, name)
		if _, taken := t.index[name]; !taken {
			t.index[name] = i
		}
	}
	for {
		cells, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.corrupt++
			continue
		}
		if isBlank(cells) {
			continue
		}
		for i, c := range cells {
			cells[i] = strings.ReplaceAll(c, "\r", "")
		}
		t.rows = append(t.rows, row{cells: cells, index: t.index})
	}
	return t, nil
}

// has reports whether the file carries a column, which is how a reader tells a
// file meant for it from one that is not.
func (t *table) has(columns ...string) bool {
	for _, name := range columns {
		if _, ok := t.index[name]; !ok {
			return false
		}
	}
	return true
}

// warning is what to say about the lines that would not come apart.
func (t *table) warning() string {
	if t.corrupt == 0 {
		return ""
	}
	if t.corrupt == 1 {
		return "1 line of the file was not readable and was left out."
	}
	return fmt.Sprintf("%d lines of the file were not readable and were left out.", t.corrupt)
}

func (r row) get(column string) string {
	i, ok := r.index[column]
	if !ok || i >= len(r.cells) {
		return ""
	}
	return r.cells[i]
}

// first is the value of whichever of these columns the file has, which is how
// one reader covers a manager that renamed a column between versions.
func (r row) first(columns ...string) string {
	for _, name := range columns {
		if v := r.get(name); v != "" {
			return v
		}
	}
	return ""
}

func (r row) number(column string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(r.get(column)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func isBlank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

// groupByVault keeps the order the file had, so two runs of the same file say
// the same thing.
type grouped struct {
	names []string
	items map[string][]Entry
}

func newGrouped() *grouped { return &grouped{items: map[string][]Entry{}} }

func (g *grouped) add(vault string, entries ...Entry) {
	if _, seen := g.items[vault]; !seen {
		g.names = append(g.names, vault)
	}
	g.items[vault] = append(g.items[vault], entries...)
}

func (g *grouped) into(doc *Document) {
	for _, name := range g.names {
		doc.vault(name, g.items[name])
	}
}
