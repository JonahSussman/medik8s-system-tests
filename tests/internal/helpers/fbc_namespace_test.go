package helpers

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func fbcNamespaceClient(objects ...client.Object) client.WithWatch {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestFBCNamespaceRejectsExistingInstallation(t *testing.T) {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription",
	})
	object.SetName("arbitrary-subscription")
	object.SetNamespace("elsewhere")
	object.Object["spec"] = map[string]interface{}{"name": "self-node-remediation"}
	run := &FBCNamespace{API: fbcNamespaceClient(object), Name: "new-namespace", Token: "new-run"}
	if err := run.CheckClean(context.Background(), "self-node-remediation"); err == nil {
		t.Fatal("accepted an existing SNR Subscription")
	}

	for _, failure := range []error{
		errors.New("network unavailable"),
		apierrors.NewForbidden(schema.GroupResource{Resource: "subscriptions"}, "", errors.New("denied")),
	} {
		run.API = interceptor.NewClient(fbcNamespaceClient(), interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return failure
			},
		})
		if err := run.CheckClean(context.Background(), "self-node-remediation"); err == nil {
			t.Fatal("hid a transport or authorization failure")
		}
	}
}

func TestFBCNamespaceCleanupPreservesUnownedNamespace(t *testing.T) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "existing", UID: "existing-uid", Labels: map[string]string{FBCRunLabel: "existing-run"},
	}}
	api := fbcNamespaceClient(namespace)
	run := &FBCNamespace{API: api, Name: namespace.Name, Token: "new-run"}
	if err := run.CheckClean(context.Background(), "self-node-remediation"); err == nil {
		t.Fatal("accepted a pre-existing namespace")
	}
	if err := run.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	run.UID = "different-uid"
	if err := run.Cleanup(context.Background()); err == nil {
		t.Fatal("accepted a replaced namespace identity")
	}
	if err := api.Get(context.Background(), client.ObjectKey{Name: namespace.Name}, &corev1.Namespace{}); err != nil {
		t.Fatalf("unowned namespace was deleted: %v", err)
	}
}

func TestFBCNamespaceCleanupOwnedNamespace(t *testing.T) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "owned", UID: "owned-uid", Labels: map[string]string{FBCRunLabel: "owned-run"},
	}}
	api := fbcNamespaceClient(namespace)
	run := &FBCNamespace{API: api, Name: namespace.Name, Token: "owned-run", UID: namespace.UID}
	if err := run.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := api.Get(context.Background(), client.ObjectKey{Name: namespace.Name}, &corev1.Namespace{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("owned namespace remains: %v", err)
	}
}
