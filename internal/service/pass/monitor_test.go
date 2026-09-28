package pass

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

type aliasBreaches map[string]int

func (a aliasBreaches) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a aliasBreaches) Decode(_ context.Context, req proton.Request, out any) error {
	for item, count := range a {
		if !strings.HasSuffix(req.Path, "/alias/"+item+"/breaches") {
			continue
		}
		breaches := make([]map[string]any, count)
		for i := range breaches {
			breaches[i] = map[string]any{
				"ID": fmt.Sprint(i), "Name": "Leak", "PublishedAt": "2026-02-14T00:00:00Z",
			}
		}
		data, err := json.Marshal(map[string]any{
			"Breaches": map[string]any{"IsEligible": true, "Count": count, "Breaches": breaches},
		})
		if err != nil {
			return err
		}
		return json.Unmarshal(data, out)
	}
	return fmt.Errorf("no answer for %s", req.Path)
}

func TestEveryAliasBreachCountLandsOnItsRow(t *testing.T) {
	items := []Item{
		{ShareID: "s", ItemID: "clean", Type: "alias", Alias: "clean@passmail.net"},
		{ShareID: "s", ItemID: "first", Type: "alias", Alias: "first@passmail.net", breached: true},
		{ShareID: "s", ItemID: "note", Type: "note"},
		{ShareID: "s", ItemID: "second", Type: "alias", Alias: "second@passmail.net", breached: true},
		{ShareID: "s", ItemID: "lost", Type: "alias", Alias: "lost@passmail.net", breached: true},
		{ShareID: "s", ItemID: "last", Type: "alias", Alias: "last@passmail.net"},
	}
	rows := aliasRows(items)
	New(aliasBreaches{"first": 3, "second": 1}, nil).fillAliasBreaches(t.Context(), rows)

	got := map[string]MonitoredAddress{}
	for _, row := range rows {
		got[row.Email] = row
	}
	for email, want := range map[string]int{
		"clean@passmail.net": 0, "first@passmail.net": 3, "second@passmail.net": 1, "last@passmail.net": 0,
	} {
		if row := got[email]; row.Breaches == nil || *row.Breaches != want {
			t.Errorf("%s has %v breaches, want %d", email, row.Breaches, want)
		}
	}
	if got["first@passmail.net"].LastBreach == 0 {
		t.Error("the alias whose breaches came back has no date for the last one")
	}
	if row, listed := got["lost@passmail.net"]; !listed || row.Breaches != nil {
		t.Errorf("the alias whose count did not come back = %+v, want it listed with no count", row)
	}
}

func TestTheWorstComeFirstAndAnUnknownCountBeforeTheClean(t *testing.T) {
	rows := []MonitoredAddress{
		{Email: "clean", Breaches: known(0)},
		{Email: "unknown"},
		{Email: "two", Breaches: known(2)},
		{Email: "five", Breaches: known(5)},
	}
	slices.SortStableFunc(rows, ByBreaches)
	var order []string
	for _, row := range rows {
		order = append(order, row.Email)
	}
	if want := []string{"five", "two", "unknown", "clean"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

type recorded struct{ sent []proton.Request }

func (r *recorded) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	r.sent = append(r.sent, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (r *recorded) Decode(_ context.Context, req proton.Request, out any) error {
	r.sent = append(r.sent, req)
	return json.Unmarshal([]byte(`{"Access": {"Plan": {"Type": "plus"}, "Monitor": {"ProtonAddress": true, "Aliases": false}}}`), out)
}

func TestAnAddressInAPausedGroupIsPaused(t *testing.T) {
	for _, tc := range []struct {
		row  MonitoredAddress
		want string
	}{
		{MonitoredAddress{Type: AddressAlias, Monitored: true, Verified: true, groupPaused: true}, StatePaused},
		{MonitoredAddress{Type: AddressAlias, Monitored: true, Verified: true}, StateWatched},
		{MonitoredAddress{Type: AddressProton, Monitored: false, Verified: true}, StatePaused},
		{MonitoredAddress{Type: AddressCustom, Monitored: true, Verified: false, groupPaused: true}, StateUnverified},
	} {
		tc.row.settle()
		if tc.row.State != tc.want {
			t.Errorf("%+v settled as %s, want %s", tc.row, tc.row.State, tc.want)
		}
	}
}

func TestTheGroupSwitchesAreReadAndWrittenAsTheWebDoes(t *testing.T) {
	r := &recorded{}
	svc := New(r, nil)
	groups, err := svc.WatchedGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !groups.Watched(AddressProton) || groups.Watched(AddressAlias) || !groups.Watched(AddressCustom) || !groups.Paid {
		t.Errorf("groups = %+v", groups)
	}
	for kind, want := range map[string]string{AddressProton: "map[ProtonAddress:true]", AddressAlias: "map[Aliases:true]"} {
		r.sent = nil
		if err := svc.WatchGroup(t.Context(), kind, true); err != nil {
			t.Fatal(err)
		}
		if got := r.sent[0]; got.Method != "PUT" || got.Path != "/pass/v1/user/monitor" || fmt.Sprint(got.Body) != want {
			t.Errorf("%s sent %s %s %v, want %s", kind, got.Method, got.Path, got.Body, want)
		}
	}
}

func TestResolvingAsksAtTheAddressesOwnPath(t *testing.T) {
	r := &recorded{}
	svc := New(r, nil)
	for _, tc := range []struct {
		address MonitoredAddress
		want    string
	}{
		{MonitoredAddress{Type: AddressProton, AddressID: "addr"}, "POST /pass/v1/breach/address/addr/resolved"},
		{MonitoredAddress{Type: AddressCustom, AddressID: "custom"}, "PUT /pass/v1/breach/custom_email/custom/resolved"},
		{MonitoredAddress{Type: AddressAlias, shareID: "s", itemID: "i"}, "POST /pass/v1/share/s/alias/i/breaches/resolved"},
	} {
		r.sent = nil
		if err := svc.ResolveBreaches(t.Context(), tc.address); err != nil {
			t.Fatal(err)
		}
		if got := r.sent[0].Method + " " + r.sent[0].Path; got != tc.want {
			t.Errorf("sent %s, want %s", got, tc.want)
		}
	}
}
