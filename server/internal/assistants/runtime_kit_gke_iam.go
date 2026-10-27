package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/iam/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// kitGKEIAM manages dedicated accounts without granting any project roles.
// Required permissions are serviceAccounts create/get/delete/getIamPolicy/setIamPolicy.
type kitGKEIAM struct {
	project, namespace  string
	workloadPoolProject string
	service             *iam.Service
	dynamic             dynamic.Interface
}
type kitGKEIAMContextKey struct{}
type kitGKEIAMAttempt struct {
	manager *kitGKEIAM
	runtime assistantRuntimeRecord
}
type kitGKEIAMIdentity struct{ subject, account, email string }

var kitGKEIAMProjectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
var kitGKEIAMSubjectPattern = regexp.MustCompile(`^[0-9]+$`)

func kitGKEIAMAccount(ksa string) string {
	digest := sha256.Sum256([]byte(ksa))
	return fmt.Sprintf("kit-wl-%x", digest[:11]) // 29 characters.
}

// kitGKEWorkloadSubject replaces the KSA subject before admission. The caller
// must reject historically admitted subjects, including withdrawn admissions.
func kitGKEWorkloadSubject(ctx context.Context, ksa string) (string, error) {
	attempt, ok := ctx.Value(kitGKEIAMContextKey{}).(*kitGKEIAMAttempt)
	if !ok || attempt.manager == nil {
		return "", errors.New("Kit GKE workload IAM provisioning is not configured")
	}
	pending, err := attempt.manager.preparePending(ctx, attempt.runtime, ksa)
	if err != nil {
		return "", err
	}
	identity, err := attempt.manager.provision(ctx, ksa)
	if _, ok := errors.AsType[kitGKEIAMCreateRejectedError](err); ok {
		// This definitive rejection did not create an account. Retain that fact
		// durably if cleanup itself fails, rather than stranding an unknown intent.
		_ = unstructured.SetNestedField(pending.Object, "true", "data", "not_created")
		_, updateErr := attempt.manager.dynamic.Resource(kitGKEIAMPendingGVR).Namespace(attempt.manager.namespace).Update(ctx, pending, metav1.UpdateOptions{})
		if updateErr == nil {
			updateErr = attempt.manager.removePending(ctx, attempt.runtime)
		}
		err = errors.Join(err, updateErr)
	}
	return identity.subject, err
}
func (m *kitGKEIAM) provision(ctx context.Context, ksa string) (kitGKEIAMIdentity, error) {
	var identity kitGKEIAMIdentity
	if !kitGKEIAMProjectPattern.MatchString(m.project) || !kitGKEIAMProjectPattern.MatchString(m.workloadPoolProject) || m.namespace == "" || ksa == "" {
		return identity, errors.New("invalid Kit GKE IAM scope")
	}
	account := kitGKEIAMAccount(ksa)
	email := account + "@" + m.project + ".iam.gserviceaccount.com"
	name := "projects/" + m.project + "/serviceAccounts/" + email
	accounts := m.service.Projects.ServiceAccounts
	sa, err := accounts.Create("projects/"+m.project, &iam.CreateServiceAccountRequest{AccountId: account, ServiceAccount: &iam.ServiceAccount{DisplayName: "Speakeasy Kit workload", Description: m.description(ksa)}}).Context(ctx).Do()
	if kitGKEIAMStatus(err, 400) || kitGKEIAMStatus(err, 403) || kitGKEIAMStatus(err, 404) || kitGKEIAMStatus(err, 429) {
		return identity, kitGKEIAMCreateRejectedError{err}
	}
	if kitGKEIAMStatus(err, 409) {
		sa, err = accounts.Get(name).Context(ctx).Do()
	}
	if err != nil {
		return identity, fmt.Errorf("create Kit GKE workload service account: %w", err)
	}
	if sa.Email != email || sa.Description != m.description(ksa) || !kitGKEIAMSubjectPattern.MatchString(sa.UniqueId) || sa.Disabled {
		return identity, errors.New("Kit GKE IAM account identity mismatch")
	}
	identity = kitGKEIAMIdentity{subject: sa.UniqueId, account: ksa, email: sa.Email}
	name = "projects/" + m.project + "/serviceAccounts/" + sa.UniqueId
	policy, err := accounts.GetIamPolicy(name).Context(ctx).Do()
	if err != nil {
		return identity, fmt.Errorf("read Kit GKE workload IAM policy: %w", err)
	}
	member := "serviceAccount:" + m.workloadPoolProject + ".svc.id.goog[" + m.namespace + "/" + ksa + "]"
	for _, binding := range policy.Bindings {
		if binding.Role != "roles/iam.workloadIdentityUser" || binding.Condition != nil || len(binding.Members) != 1 || binding.Members[0] != member {
			return identity, errors.New("Kit GKE workload service account has unexpected IAM grants")
		}
	}
	_, err = accounts.SetIamPolicy(name, &iam.SetIamPolicyRequest{Policy: &iam.Policy{Etag: policy.Etag, Version: policy.Version, Bindings: []*iam.Binding{{Role: "roles/iam.workloadIdentityUser", Members: []string{member}}}}}).Context(ctx).Do()
	if err != nil {
		return identity, fmt.Errorf("bind Kit GKE workload service account: %w", err)
	}
	return identity, nil
}
func (m *kitGKEIAM) description(ksa string) string {
	return "Speakeasy Kit workload " + m.namespace + "/" + ksa
}

type kitGKEIAMCreateRejectedError struct{ error }

func (e kitGKEIAMCreateRejectedError) Unwrap() error { return e.error }

func kitGKEIAMStatus(err error, status int) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == status
}
func (m *kitGKEIAM) remove(ctx context.Context, identity kitGKEIAMIdentity) error {
	if !kitGKEIAMSubjectPattern.MatchString(identity.subject) || identity.account == "" {
		return errors.New("refusing to delete unbound Kit GKE IAM identity")
	}
	name := "projects/" + m.project + "/serviceAccounts/" + identity.subject
	sa, err := m.service.Projects.ServiceAccounts.Get(name).Context(ctx).Do()
	if kitGKEIAMStatus(err, 404) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Kit GKE workload before deletion: %w", err)
	}
	if sa.Description != m.description(identity.account) || sa.UniqueId != identity.subject || sa.Email != kitGKEIAMAccount(identity.account)+"@"+m.project+".iam.gserviceaccount.com" {
		return errors.New("refusing to delete mismatched Kit GKE IAM identity")
	}
	_, err = m.service.Projects.ServiceAccounts.Delete(name).Context(ctx).Do()
	if kitGKEIAMStatus(err, 404) {
		return nil
	}
	return err
}
func (m *kitGKEIAM) removeRuntime(ctx context.Context, r assistantRuntimeRecord) error {
	var metadata struct {
		Workload *KitWorkloadBinding `json:"workload"`
	}
	if len(r.BackendMetadataJSON) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.BackendMetadataJSON, &metadata); err != nil {
		return err
	}
	if metadata.Workload == nil {
		return nil
	}
	return m.remove(ctx, kitGKEIAMIdentity{subject: metadata.Workload.Subject, account: metadata.Workload.ServiceAccount})
}
