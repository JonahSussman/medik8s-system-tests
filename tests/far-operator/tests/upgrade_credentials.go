package tests

import (
	"context"
	"fmt"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (hooks *farUpgradeOperatorFBCTest) provisionCredentials(ctx context.Context) error {
	hooks.credentials = &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "cloudcredential.openshift.io/v1", "kind": "CredentialsRequest",
		"metadata": map[string]interface{}{
			"name": "far-fbc-" + hooks.owned.Token, "namespace": "openshift-cloud-credential-operator",
			"labels": map[string]interface{}{helpers.FBCRunLabel: hooks.owned.Token},
		},
		"spec": map[string]interface{}{
			"serviceAccountNames": []interface{}{"fence-agents-remediation-controller-manager"},
			"secretRef": map[string]interface{}{
				"name": farparams.AWSCredentialsSecretName, "namespace": hooks.owned.Name,
			},
			"providerSpec": map[string]interface{}{
				"apiVersion": "cloudcredential.openshift.io/v1", "kind": "AWSProviderSpec",
				"statementEntries": []interface{}{map[string]interface{}{
					"action": []interface{}{
						"ec2:DescribeInstances", "ec2:StartInstances", "ec2:StopInstances", "ec2:RebootInstances",
					},
					"effect": "Allow", "resource": "*",
				}},
			},
		},
	}}
	if err := APIClient.Create(ctx, hooks.credentials); err != nil {
		return fmt.Errorf("create owned AWS fencing CredentialsRequest: %w", err)
	}
	var accessKey, secretKey string
	Eventually(func() error {
		var err error
		accessKey, secretKey, err = farutils.GetAWSCredentials(ctx, APIClient, hooks.owned.Name)

		return err
	}, medik8sparams.DefaultTimeout, farparams.DefaultPollInterval).Should(Succeed(),
		"CCO must provision EC2 fencing credentials; manual-mode clusters need a supported credential setup")
	// Only references to this Secret appear in CRs and reports, never these values.
	return APIClient.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: farparams.SharedCredentialsSecretName, Namespace: hooks.owned.Name,
			Labels: map[string]string{helpers.FBCRunLabel: hooks.owned.Token},
		},
		StringData: map[string]string{"--access-key": accessKey, "--secret-key": secretKey},
	})
}

func (hooks *farUpgradeOperatorFBCTest) deleteCredentialsRequest(ctx context.Context) error {
	if hooks.credentials == nil || hooks.credentials.GetUID() == "" {
		return nil
	}
	object := hooks.credentials.DeepCopy()
	if err := APIClient.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
		return client.IgnoreNotFound(err)
	}
	if err := farutils.VerifyOwnedObject(object, hooks.credentials.GetUID(), hooks.owned.Token); err != nil {
		return err
	}
	uid := object.GetUID()
	if err := client.IgnoreNotFound(APIClient.Delete(ctx, object, client.Preconditions{UID: &uid})); err != nil {
		return err
	}
	// Allow CCO to revoke the run's IAM credentials before deleting their namespace.
	return wait.PollUntilContextTimeout(ctx, farparams.DefaultPollInterval,
		medik8sparams.DefaultTimeout, true, func(ctx context.Context) (bool, error) {
			err := APIClient.Get(ctx, client.ObjectKeyFromObject(object), object)

			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		})
}
