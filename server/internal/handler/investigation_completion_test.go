package handler

import "testing"

func TestParseInvestigationResult(t *testing.T) {
	tests := []struct {
		name   string
		output string
		kind   string
		ok     bool
	}{
		{
			name: "conclusion",
			output: "analysis complete\nINVESTIGATION_RESULT_JSON:\n```json\n" +
				`{"kind":"conclusion","root_cause":"connection pool exhaustion","evidence":[{"source":"metrics","detail":"pool at limit"}],"confidence":"confirmed","category":"database","recommendations":["raise pool limit"],"open_questions":[]}` + "\n```",
			kind: "conclusion", ok: true,
		},
		{
			name:   "needs input",
			output: `INVESTIGATION_RESULT_JSON: {"kind":"needs_input","question":"What was the request ID?"}`,
			kind:   "needs_input", ok: true,
		},
		{
			name:   "invalid confidence",
			output: `INVESTIGATION_RESULT_JSON: {"kind":"conclusion","root_cause":"x","evidence":[],"confidence":"certain","recommendations":[],"open_questions":[]}`,
			ok:     false,
		},
		{
			name:   "evidence must be a non-empty array",
			output: `INVESTIGATION_RESULT_JSON: {"kind":"conclusion","root_cause":"x","evidence":{"source":"logs"},"confidence":"confirmed"}`,
			ok:     false,
		},
		{
			name:   "missing question",
			output: `INVESTIGATION_RESULT_JSON: {"kind":"needs_input"}`,
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInvestigationResult(tt.output)
			if (err == nil) != tt.ok {
				t.Fatalf("error = %v", err)
			}
			if got.Kind != tt.kind {
				t.Fatalf("kind = %q", got.Kind)
			}
		})
	}
}
