package snrutils

import (
	"fmt"
	"strconv"
	"time"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// VerifyAgentConfiguration checks that the config probe reached the agent pod template.
func VerifyAgentConfiguration(podSpec *corev1.PodSpec, spec map[string]interface{}, token string) error {
	interval, _, err := unstructured.NestedString(spec, "peerUpdateInterval")
	if err != nil {
		return err
	}
	duration, err := time.ParseDuration(interval)
	if err != nil {
		return err
	}
	threshold, _, err := unstructured.NestedInt64(spec, "maxApiErrorThreshold")
	if err != nil {
		return err
	}
	expected := map[string]string{
		"PEER_UPDATE_INTERVAL":    strconv.FormatInt(duration.Nanoseconds(), 10),
		"MAX_API_ERROR_THRESHOLD": strconv.FormatInt(threshold, 10),
	}

	for _, container := range podSpec.Containers {
		if container.Name != snrparams.ManagerContainerName {
			continue
		}

		for _, env := range container.Env {
			if value, found := expected[env.Name]; found && env.Value == value {
				delete(expected, env.Name)
			}
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("agent config environment has not reconciled: expected %v", expected)
	}
	tolerations, _, err := unstructured.NestedSlice(spec, "customDsTolerations")
	if err != nil {
		return err
	}
	expectedProbe := false

	for _, value := range tolerations {
		toleration, ok := value.(map[string]interface{})
		if ok && toleration["key"] == helpers.FBCRunLabel && toleration["value"] == token {
			expectedProbe = true
		}
	}
	observedProbe := false

	for _, toleration := range podSpec.Tolerations {
		if toleration.Key == helpers.FBCRunLabel && toleration.Value == token {
			observedProbe = true
		}
	}
	if expectedProbe != observedProbe {
		return fmt.Errorf("agent probe toleration not reconciled: expected=%t observed=%t", expectedProbe, observedProbe)
	}

	return nil
}
