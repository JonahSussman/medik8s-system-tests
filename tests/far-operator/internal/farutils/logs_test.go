package farutils

import "testing"

func TestCountRemediationFailureLogs(t *testing.T) {
	const expectedFailures = 2
	logs := `{"msg":"command failed","uid":"target"}
2026-10-06 INFO executer command failed {"uid": "target", "errMessage": "command failed"}
{"msg":"command failed","uid":"other"}
{"msg":"command failed","uid":"target-suffix"}
{"msg":"command completed","uid":"target"}
{"msg":"command failed"}`
	if got := CountRemediationFailureLogs(logs, "target"); got != expectedFailures {
		t.Fatalf("count = %d, want %d", got, expectedFailures)
	}

	if got := CountRemediationFailureLogs(logs, ""); got != 0 {
		t.Fatalf("empty UID counted %d lines", got)
	}
}
