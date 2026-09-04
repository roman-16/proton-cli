package live

import (
	"encoding/json"
	"fmt"
	"testing"
)

// The raw escape hatch, for reaching what no command models.
//
// It proves nothing about the surface the CLI itself covers, which is why the
// coverage recording leaves what it sent out.

// Raw api escape-hatch tests.

func TestAPIGet(t *testing.T) {
	stdout := runOK(t, "api", "GET", "/core/v4/users")
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if _, ok := v["User"]; !ok {
		t.Error("expected User key in response")
	}
}

func TestAPIGetWithQuery(t *testing.T) {
	stdout := runOK(t, "api", "GET", "/mail/v4/messages",
		"--query", "Page=0", "--query", "PageSize=1")
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if code, ok := v["Code"].(float64); !ok || int(code) != 1000 {
		t.Errorf("expected Code 1000, got %v", v["Code"])
	}
}

func TestAPIPostDeleteRoundTrip(t *testing.T) {
	name := testID() + "-api"
	body := fmt.Sprintf(`{"Name":%q,"Color":"#8080FF","Type":1}`, name)
	stdout := runOK(t, "api", "POST", "/core/v4/labels", "--body", body)

	var v map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("create: not valid JSON: %v", err)
	}
	labelObj, _ := v["Label"].(map[string]interface{})
	id, _ := labelObj["ID"].(string)
	if id == "" {
		t.Fatal("no Label.ID in response")
	}
	cleanupRun(t, "Delete label: proton api DELETE /core/v4/labels --body ...",
		"api", "DELETE", "/core/v4/labels",
		"--body", fmt.Sprintf(`{"LabelIDs":[%q]}`, id))

	// Delete via api (this is the assert - round-trip must not error)
	stdout2 := runOK(t, "api", "DELETE", "/core/v4/labels",
		"--body", fmt.Sprintf(`{"LabelIDs":[%q]}`, id))
	var v2 map[string]interface{}
	if err := json.Unmarshal([]byte(stdout2), &v2); err != nil {
		t.Fatalf("delete: not valid JSON: %v", err)
	}
	// Code 1001 = multi-response OK. Accept that or 1000.
	code, _ := v2["Code"].(float64)
	if int(code) != 1000 && int(code) != 1001 {
		t.Errorf("expected Code 1000/1001, got %v", v2["Code"])
	}
}
