package mdrutils

import (
	"fmt"
	"reflect"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// VerifyConfiguration checks identity and the full persisted template spec, including empty maps.
func VerifyConfiguration(object *unstructured.Unstructured, uid types.UID, spec map[string]interface{}) error {
	if uid == "" || object.GetUID() != uid {
		return fmt.Errorf("MDR template UID changed: expected %s, got %s", uid, object.GetUID())
	}
	current, found, err := unstructured.NestedMap(object.Object, "spec")
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(current, spec) {
		return fmt.Errorf("MDR template spec changed: expected %v, got %v", spec, current)
	}

	return nil
}

// SafeProbe copies the persisted template and blocks Machine deletion while the request is active.
// NHC is not installed: its timeout annotation is a supported controller safety gate.
func SafeProbe(template *unstructured.Unstructured, name, token string) (*unstructured.Unstructured, error) {
	spec, found, err := unstructured.NestedMap(template.Object, "spec", "template", "spec")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("MDR template must contain template.spec")
	}
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": mdrparams.CRDGroup + "/" + mdrparams.CRDVersion,
		"kind":       "MachineDeletionRemediation", "spec": spec,
	}}
	object.SetName(name)
	object.SetNamespace(template.GetNamespace())
	object.SetLabels(map[string]string{helpers.FBCRunLabel: token})
	object.SetAnnotations(map[string]string{
		mdrparams.NHCTimedOutAnnotationKey: mdrparams.NHCTimedOutAnnotationValue,
	})

	return object, nil
}

// VerifySafeProbe requires both stopped conditions on the newly created, owned request.
// MDR does not populate observedGeneration; a fresh request UID prevents stale GA status passing.
func VerifySafeProbe(object *unstructured.Unstructured, uid types.UID, token string) error {
	if uid == "" || object.GetUID() != uid || object.GetLabels()[helpers.FBCRunLabel] != token ||
		object.GetDeletionTimestamp() != nil {
		return fmt.Errorf("MDR probe was replaced or is not owned by this run")
	}
	if _, exists := object.GetAnnotations()[mdrparams.NHCTimedOutAnnotationKey]; !exists {
		return fmt.Errorf("MDR probe is missing its mandatory safety annotation")
	}
	conditions, _, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil {
		return err
	}
	for _, conditionType := range []string{mdrparams.ProcessingConditionType, mdrparams.SucceededConditionType} {
		matched := false
		for _, value := range conditions {
			condition, ok := value.(map[string]interface{})
			if ok && condition["type"] == conditionType && condition["status"] == "False" &&
				condition["reason"] == mdrparams.ConditionReasonStoppedByNHC {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("MDR probe has no %s=False/%s response", conditionType, mdrparams.ConditionReasonStoppedByNHC)
		}
	}

	return nil
}
