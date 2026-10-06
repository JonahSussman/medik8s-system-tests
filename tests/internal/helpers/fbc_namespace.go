package helpers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FBCRunLabel marks a namespace created exclusively for one FBC upgrade run.
const FBCRunLabel = "test.medik8s.io/fbc-upgrade-run"

// FBCNamespace owns a newly created namespace, never an existing installation.
// Shared CRDs are retained. OLM-created cluster resources are identified by
// their namespace ownership label and deleted with UID preconditions.
type FBCNamespace struct {
	API   client.Client
	Name  string
	Token string
	UID   types.UID
}

// MissingFBCAPI allows an absent operator API, but not authorization or transport failures.
func MissingFBCAPI(err error) bool {
	return apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
}

// CheckClean rejects existing namespaces and installations of the target package.
func (run *FBCNamespace) CheckClean(ctx context.Context, packageName string, kinds ...schema.GroupVersionKind) error {
	namespace := &corev1.Namespace{}
	if err := run.API.Get(ctx, client.ObjectKey{Name: run.Name}, namespace); !apierrors.IsNotFound(err) {
		if err != nil {
			return err
		}

		return fmt.Errorf("namespace %s already exists; this run does not own it", run.Name)
	}
	objects, err := run.clusterObjects(ctx)
	if err != nil {
		return err
	}
	if len(objects) != 0 {
		return fmt.Errorf("pre-existing OLM cluster resources reference namespace %s", run.Name)
	}
	kinds = append(kinds,
		schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription"},
		schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "ClusterServiceVersion"},
		schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "InstallPlan"},
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"})

	for _, kind := range kinds {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(kind.GroupVersion().WithKind(kind.Kind + "List"))
		if err := run.API.List(ctx, list); err != nil {
			if MissingFBCAPI(err) {
				continue
			}

			return err
		}

		for _, object := range list.Items {
			identity := object.GetName()
			if kind.Group == "operators.coreos.com" {
				identity += fmt.Sprint(object.Object["spec"])
			}
			if strings.Contains(identity, packageName) || strings.HasSuffix(kind.Group, ".medik8s.io") {
				return fmt.Errorf("pre-existing %s %s/%s is not owned by this run",
					kind.Kind, object.GetNamespace(), object.GetName())
			}
		}
	}

	return nil
}

// Create acquires the namespace with a unique marker before any installation.
func (run *FBCNamespace) Create(ctx context.Context) error {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: run.Name, Labels: map[string]string{
		FBCRunLabel: run.Token,
		"security.openshift.io/scc.podSecurityLabelSync": "false",
		"pod-security.kubernetes.io/enforce":             "privileged",
	}}}
	err := run.API.Create(ctx, namespace)
	if err == nil {
		run.UID = namespace.UID
	}

	return err
}

// VerifyOwner protects every mutation and cleanup against a replaced namespace.
func (run *FBCNamespace) VerifyOwner(ctx context.Context) error {
	namespace := &corev1.Namespace{}
	if err := run.API.Get(ctx, client.ObjectKey{Name: run.Name}, namespace); err != nil {
		return err
	}
	if run.UID == "" || namespace.UID != run.UID || namespace.Labels[FBCRunLabel] != run.Token {
		return fmt.Errorf("refusing mutation of unowned/replaced namespace %s", run.Name)
	}

	return nil
}

// Cleanup removes only this namespace and its explicitly OLM-owned cluster objects.
func (run *FBCNamespace) Cleanup(ctx context.Context) error {
	if run.UID == "" {
		return nil
	}
	if err := run.VerifyOwner(ctx); err != nil {
		return client.IgnoreNotFound(err)
	}
	objects, err := run.clusterObjects(ctx)
	if err != nil {
		return err
	}
	var failures []error

	for _, object := range objects {
		uid := object.GetUID()
		if uid == "" {
			failures = append(failures, fmt.Errorf("refusing deletion without UID: %s", object.GetName()))

			continue
		}
		if err := client.IgnoreNotFound(run.API.Delete(ctx, object, client.Preconditions{UID: &uid})); err != nil {
			failures = append(failures, err)
		}
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: run.Name}}
	if err := client.IgnoreNotFound(run.API.Delete(ctx, namespace, client.Preconditions{UID: &run.UID})); err != nil {
		failures = append(failures, err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 10*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			err := run.API.Get(ctx, client.ObjectKey{Name: run.Name}, &corev1.Namespace{})
			if !apierrors.IsNotFound(err) {
				return false, err
			}
			remaining, err := run.clusterObjects(ctx)

			return len(remaining) == 0, err
		}); err != nil {
		failures = append(failures, fmt.Errorf("owned namespace/OLM resources remain: %w", err))
	}

	return errors.Join(failures...)
}

func (run *FBCNamespace) clusterObjects(ctx context.Context) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	for _, kind := range []schema.GroupVersionKind{
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "MutatingWebhookConfiguration"},
	} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(kind.GroupVersion().WithKind(kind.Kind + "List"))
		if err := run.API.List(ctx, list, client.MatchingLabels{"olm.owner.namespace": run.Name}); err != nil {
			return nil, err
		}

		for i := range list.Items {
			objects = append(objects, list.Items[i].DeepCopy())
		}
	}

	return objects, nil
}
