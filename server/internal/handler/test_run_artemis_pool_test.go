package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCaseCapabilityKeysRotatesPooledKindsOnly(t *testing.T) {
	binding := TestRunCapabilityBinding{
		Resolved: map[string]string{"android_device": "android:A", "browser": "browser:playwright"},
		Pools:    map[string][]string{"android_device": {"android:A", "android:B", "android:C"}},
	}
	seen := map[string]int{}
	for pos := int32(0); pos < 6; pos++ {
		keys := caseCapabilityKeys(binding, "run-1", pos)
		if keys["browser"] != "browser:playwright" {
			t.Errorf("position %d: a kind without a pool must keep its binding, got %q", pos, keys["browser"])
		}
		seen[keys["android_device"]]++
		next := caseCapabilityKeys(binding, "run-1", pos+1)
		if next["android_device"] == keys["android_device"] {
			t.Errorf("positions %d and %d share %s; neighbours must land on different phones", pos, pos+1, keys["android_device"])
		}
	}
	if len(seen) != 3 || seen["android:A"] != 2 || seen["android:B"] != 2 || seen["android:C"] != 2 {
		t.Errorf("six cases over three phones = %v, want two each", seen)
	}
	if a, b := caseCapabilityKeys(binding, "run-1", 4), caseCapabilityKeys(binding, "run-1", 4); a["android_device"] != b["android_device"] {
		t.Errorf("the assignment must be stable for a case: %v vs %v", a, b)
	}
	unpooled := caseCapabilityKeys(TestRunCapabilityBinding{Resolved: map[string]string{"ios_device": "ios:X"}}, "run-1", 3)
	if unpooled["ios_device"] != "ios:X" {
		t.Errorf("a binding without pools must pass through, got %v", unpooled)
	}
}

// A round of Android cases on an Artemis test host freezes every matching
// phone into the binding, and each case task is pinned to one of them: the
// overlay's --serial and the context's assigned capability name the same phone,
// the phone that fails the case constraint is never used, and the round's
// cases do not all queue on the first phone.
func TestDispatchTestRunSpreadsAndroidCasesOverTheArtemisPool(t *testing.T) {
	projectID := newTestRunProject(t)
	var caseIDs []string
	for i := 0; i < 3; i++ {
		var tc TestCaseResponse
		testutil.Call(t, testHandler.CreateTestCase, newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
			"project_id":            projectID,
			"title":                 "Artemis case " + t.Name(),
			"status":                "active",
			"required_capabilities": []map[string]any{{"kind": "android_device", "match": map[string]string{"os_version": ">=13"}}},
		})).Want(http.StatusCreated).JSON(&tc)
		caseIDs = append(caseIDs, tc.ID)
	}

	runtimeID := dbfx.Runtime(t, "artemis-pool-runtime", testutil.Cols{"daemon_id": "daemon-artemis-pool", "test_host_enabled": true})
	agentID := dbfx.Agent(t, "artemis-pool-agent", runtimeID)
	for serial, osVersion := range map[string]string{"PHONE-A": "14", "PHONE-B": "13", "PHONE-OLD": "12"} {
		target, _ := json.Marshal(map[string]string{
			"platform":       "android",
			"provider":       "artemis",
			"serial":         serial,
			"os_version":     osVersion,
			"artemis_python": "/opt/artemis/.venv/bin/python",
			"artemis_server": "/opt/artemis/mcp_server/server.py",
			"multica_cli":    "/usr/local/bin/multica",
		})
		dbfx.Insert(t, "test_capability", testutil.Cols{
			"workspace_id":   testWorkspaceID,
			"daemon_id":      "daemon-artemis-pool",
			"runtime_id":     runtimeID,
			"kind":           "android_device",
			"capability_key": "android:" + serial,
			"target":         string(target),
			"status":         "available",
		})
	}

	run := createTestRunFromCases(t, "Artemis pool run", caseIDs)
	w := dispatchRun(t, run.ID, agentID)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d: %s", w.Code, w.Body.String())
	}

	var bindingJSON []byte
	if err := testPool.QueryRow(context.Background(), `SELECT capability_binding FROM test_run WHERE id = $1`, run.ID).Scan(&bindingJSON); err != nil {
		t.Fatal(err)
	}
	var binding TestRunCapabilityBinding
	if err := json.Unmarshal(bindingJSON, &binding); err != nil {
		t.Fatal(err)
	}
	if pool := binding.Pools["android_device"]; len(pool) != 2 || pool[0] != "android:PHONE-A" || pool[1] != "android:PHONE-B" {
		t.Fatalf("frozen pool = %v, want the two phones satisfying os_version >=13", binding.Pools)
	}

	rows, err := testPool.Query(context.Background(), `
		SELECT q.context, q.runtime_mcp_overlay
		FROM test_run_case rc JOIN agent_task_queue q ON q.id = rc.agent_task_id
		WHERE rc.run_id = $1 ORDER BY rc.position`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	used := map[string]int{}
	tasks := 0
	for rows.Next() {
		var contextJSON, overlay []byte
		if err := rows.Scan(&contextJSON, &overlay); err != nil {
			t.Fatal(err)
		}
		tasks++
		var runCtx struct {
			Assigned map[string]string `json:"assigned_capabilities"`
		}
		if err := json.Unmarshal(contextJSON, &runCtx); err != nil {
			t.Fatal(err)
		}
		var mounted struct {
			Servers map[string]struct {
				Args []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(overlay, &mounted); err != nil {
			t.Fatal(err)
		}
		serial := ""
		args := mounted.Servers["artemis"].Args
		for i, a := range args {
			if a == "--serial" && i+1 < len(args) {
				serial = args[i+1]
			}
		}
		if serial == "" {
			t.Fatalf("overlay mounts no pinned Artemis: %s", overlay)
		}
		if runCtx.Assigned["android_device"] != "android:"+serial {
			t.Errorf("context assigns %q but the overlay pins %q", runCtx.Assigned["android_device"], serial)
		}
		if serial == "PHONE-OLD" {
			t.Errorf("a case was pinned to the phone that fails its constraint")
		}
		used[serial]++
	}
	if tasks != 3 {
		t.Fatalf("case tasks = %d, want 3", tasks)
	}
	if len(used) != 2 {
		t.Errorf("phones used = %v, want the round spread over both matching phones", used)
	}
}
