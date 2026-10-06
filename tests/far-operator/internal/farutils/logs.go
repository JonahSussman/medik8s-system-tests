package farutils

import (
	"regexp"
	"strings"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
)

// CountRemediationFailureLogs counts failed fence commands for one FAR CR.
// The executer's structured uid field is emitted in both JSON and console logs.
func CountRemediationFailureLogs(logs, uid string) int {
	if uid == "" {
		return 0
	}

	uidPattern := regexp.MustCompile(`"uid"\s*:\s*"` + regexp.QuoteMeta(uid) + `"`)
	failurePattern := regexp.MustCompile(farparams.TimedOutLogPattern)
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		if uidPattern.MatchString(line) && failurePattern.MatchString(line) {
			count++
		}
	}

	return count
}
