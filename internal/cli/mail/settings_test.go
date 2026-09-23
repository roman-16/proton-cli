package mail

import (
	"encoding/json"
	"testing"
)

func TestGetReadsEachSettingTheWayTheWebDoes(t *testing.T) {
	var stored storedSettings
	if err := json.Unmarshal([]byte(`{
		"AutoDeleteSpamAndTrashDays": null,
		"BlockSenderConfirmation": null,
		"FontFace": null,
		"FontSize": null,
		"ImageProxy": 3,
		"MailCategoryView": true,
		"MailCategoryViewCountersEnabled": false,
		"NextMessageOnMove": 2,
		"PMSignature": 3,
		"RemoveImageMetadata": true,
		"SpamAction": null
	}`), &stored); err != nil {
		t.Fatal(err)
	}
	view := stored.view()
	for field, tc := range map[string]struct{ got, want string }{
		"auto_delete_spam_trash":    {view.AutoDeleteSpamTrash, "off"},
		"block_sender_confirmation": {view.BlockSenderConfirmation, "on"},
		"category_view":             {view.CategoryView, "on"},
		"category_view_counters":    {view.CategoryViewCounters, "off"},
		"font_face":                 {view.FontFace, "arial"},
		"font_size":                 {view.FontSize, "14"},
		"image_proxy":               {view.ImageProxy, "on"},
		"next_message_on_move":      {view.NextMessageOnMove, "on"},
		"pm_signature":              {view.PMSignature, "on"},
		"remove_image_metadata":     {view.RemoveImageMetadata, "on"},
		"spam_action":               {view.SpamAction, "ask"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", field, tc.got, tc.want)
		}
	}
}

func TestGetNamesWhatTheWebStoresForAChoice(t *testing.T) {
	var stored storedSettings
	if err := json.Unmarshal([]byte(`{
		"BlockSenderConfirmation": 1,
		"FontFace": "Menlo, Consolas, Courier New, Monospace",
		"FontSize": 18,
		"ImageProxy": 1,
		"SpamAction": 1
	}`), &stored); err != nil {
		t.Fatal(err)
	}
	view := stored.view()
	for field, tc := range map[string]struct{ got, want string }{
		"block_sender_confirmation": {view.BlockSenderConfirmation, "off"},
		"font_face":                 {view.FontFace, "monospace"},
		"font_size":                 {view.FontSize, "18"},
		"image_proxy":               {view.ImageProxy, "off"},
		"spam_action":               {view.SpamAction, "move-and-unsubscribe"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", field, tc.got, tc.want)
		}
	}
}

func TestImageProxyWritesTheWebsBitsInTheWebsOrder(t *testing.T) {
	off, err := specs["image-proxy"].Parse("image-proxy", "off")
	if err != nil {
		t.Fatal(err)
	}
	if len(off.Bodies) != 2 || off.Bodies[0]["ImageProxy"] != storeRemoteImages ||
		off.Bodies[1]["ImageProxy"] != proxyRemoteImages {
		t.Errorf("off sends %v, want the store bit removed and then the proxy bit", off.Bodies)
	}
	on, err := specs["image-proxy"].Parse("image-proxy", "on")
	if err != nil {
		t.Fatal(err)
	}
	if len(on.Bodies) != 1 || on.Bodies[0]["ImageProxy"] != proxyRemoteImages || on.Bodies[0]["Action"] != 1 {
		t.Errorf("on sends %v, want the proxy bit added", on.Bodies)
	}
}
