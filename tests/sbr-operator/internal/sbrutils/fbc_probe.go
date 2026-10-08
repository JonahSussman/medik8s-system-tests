package sbrutils

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// VerifyConfiguration requires the original object identity and complete specification.
func VerifyConfiguration(object *unstructured.Unstructured, uid types.UID, spec map[string]interface{}) error {
	if uid == "" || object.GetUID() != uid {
		return fmt.Errorf("StorageBasedRemediationConfig UID changed: expected %s, got %s", uid, object.GetUID())
	}

	current, found, err := unstructured.NestedMap(object.Object, "spec")
	if err != nil {
		return err
	}

	if !found || !reflect.DeepEqual(current, spec) {
		return fmt.Errorf("StorageBasedRemediationConfig specification changed: expected %v, got %v", spec, current)
	}

	return nil
}
