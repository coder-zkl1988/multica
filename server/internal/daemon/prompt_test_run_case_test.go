package daemon

import (
	"strings"
	"testing"
)

// A case gets the driving rules of the phone it was bound to and no other:
// Android goes to Artemis (with the phone named), an iPhone is driven frame by
// frame through the hub.
func TestBuildTestRunCasePrompt_RulesFollowTheBoundPhone(t *testing.T) {
	android := buildTestRunPrompt(Task{TestRunContext: []byte(`{"type":"test_run","run_id":"r","run_case_id":"c","case_key":"TC-7",
		"capability_binding":{"resolved":{"android_device":"android:PHONE-A"}},
		"assigned_capabilities":{"android_device":"android:PHONE-B"}}`)})
	for _, want := range []string{"TC-7", "Your phone is `PHONE-B`", "mobile_run_task", "mobile_manage_task", "mobile_inspect_trace", "NOT a pass"} {
		if !strings.Contains(android, want) {
			t.Errorf("Android case prompt lacks %q", want)
		}
	}
	// The round's first match is in the context JSON; the rules name the
	// phone this case was assigned.
	for _, unwanted := range []string{"Your phone is `PHONE-A`", "Driving the iPhone", "`device_info` first"} {
		if strings.Contains(android, unwanted) {
			t.Errorf("Android case prompt must not mention %q", unwanted)
		}
	}

	// A context from before per-case assignment falls back to the round's key.
	legacy := buildTestRunPrompt(Task{TestRunContext: []byte(`{"run_id":"r","run_case_id":"c","capability_binding":{"resolved":{"android_device":"android:PHONE-A"}}}`)})
	if !strings.Contains(legacy, "Your phone is `PHONE-A`") {
		t.Error("a context without assigned_capabilities must use the round's binding")
	}

	iphone := buildTestRunPrompt(Task{TestRunContext: []byte(`{"run_id":"r","run_case_id":"c","assigned_capabilities":{"ios_device":"ios:00008110"}}`)})
	if !strings.Contains(iphone, "Driving the iPhone through `multica-device`") || strings.Contains(iphone, "mobile_run_task") {
		t.Errorf("iPhone case prompt = %s", iphone)
	}

	browser := buildTestRunPrompt(Task{TestRunContext: []byte(`{"run_id":"r","run_case_id":"c","assigned_capabilities":{"browser":"browser:playwright"}}`)})
	if strings.Contains(browser, "Driving the") {
		t.Errorf("a browser case must not get phone rules: %s", browser)
	}
}
