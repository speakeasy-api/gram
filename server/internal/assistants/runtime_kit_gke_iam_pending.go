package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var kitGKEIAMPendingGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

// The pending record precedes the external create. Unlike transactional runtime
// metadata it survives a process crash or rollback during bootstrap. One record
// per lineage serializes under the existing admission lock; it is never replaced
// until the previous account has been deleted. It contains no credentials.
func (m *kitGKEIAM) preparePending(ctx context.Context, r assistantRuntimeRecord, ksa string) (*unstructured.Unstructured, error) {
	if m.dynamic == nil || r.ID == uuid.Nil || r.LifecycleTx == nil {
		return nil, errors.New("kit GKE IAM requires durable pending evidence and lifecycle lock")
	}
	if err := m.removePending(ctx, r); err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": kitGKEName(r) + "-iam", "namespace": m.namespace},
		"data":     map[string]any{"runtime": r.ID.String(), "assistant": r.AssistantID.String(), "project": r.ProjectID.String(), "iam_project": m.project, "account": ksa},
	}}
	created, err := m.dynamic.Resource(kitGKEIAMPendingGVR).Namespace(m.namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("record pending Kit GKE IAM create: %w", err)
	}
	return created, nil
}

func (m *kitGKEIAM) removePending(ctx context.Context, r assistantRuntimeRecord) error {
	if m.dynamic == nil {
		return errors.New("kit GKE IAM pending evidence client is missing")
	}
	resource := m.dynamic.Resource(kitGKEIAMPendingGVR).Namespace(m.namespace)
	obj, err := resource.Get(ctx, kitGKEName(r)+"-iam", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending Kit GKE IAM create: %w", err)
	}
	data, _, err := unstructured.NestedStringMap(obj.Object, "data")
	if err != nil || obj.GetUID() == "" || data["runtime"] != r.ID.String() || data["assistant"] != r.AssistantID.String() || data["project"] != r.ProjectID.String() || data["iam_project"] != m.project || data["account"] == "" {
		return errors.New("pending Kit GKE IAM ownership mismatch")
	}
	if data["not_created"] == "true" {
		return m.deletePending(ctx, obj)
	}
	account := data["account"]
	// Resolve uncertain create outcomes by deterministic email, but delete only
	// after checking the dedicated description and Google's immutable numeric ID.
	sa, err := m.service.Projects.ServiceAccounts.Get("projects/" + m.project + "/serviceAccounts/" + kitGKEIAMAccount(account) + "@" + m.project + ".iam.gserviceaccount.com").Context(ctx).Do()
	if kitGKEIAMStatus(err, 404) {
		var metadata struct {
			Workload *KitWorkloadBinding `json:"workload"`
		}
		_ = json.Unmarshal(r.BackendMetadataJSON, &metadata)
		// A persisted numeric binding proves this was an observed successful create;
		// an unobserved create may still be in flight. Keep that intent for recovery.
		if !kitGKEIAMSubjectPattern.MatchString(data["subject"]) && (metadata.Workload == nil || metadata.Workload.ServiceAccount != account || !kitGKEIAMSubjectPattern.MatchString(metadata.Workload.Subject)) {
			return errors.New("pending Kit GKE IAM create remains uncertain; retry cleanup or reconcile pending evidence")
		}
	} else if err != nil {
		return fmt.Errorf("resolve pending Kit GKE IAM create: %w", err)
	} else {
		if sa.Description != m.description(account) || (data["subject"] != "" && data["subject"] != sa.UniqueId) {
			return errors.New("pending Kit GKE IAM account ownership mismatch")
		}
		// Persist the immutable subject before deletion. A deletion that succeeds
		// remotely followed by a failed journal delete must remain recoverable.
		data["subject"] = sa.UniqueId
		if err := unstructured.SetNestedStringMap(obj.Object, data, "data"); err != nil {
			return fmt.Errorf("set pending Kit GKE IAM subject: %w", err)
		}
		obj, err = resource.Update(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("record pending Kit GKE IAM deletion: %w", err)
		}
		if err := m.remove(ctx, kitGKEIAMIdentity{account: account, subject: sa.UniqueId, email: ""}); err != nil {
			return err
		}
	}
	return m.deletePending(ctx, obj)
}

func (m *kitGKEIAM) deletePending(ctx context.Context, obj *unstructured.Unstructured) error {
	uid, version := obj.GetUID(), obj.GetResourceVersion()
	err := m.dynamic.Resource(kitGKEIAMPendingGVR).Namespace(m.namespace).Delete(ctx, obj.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete pending Kit GKE IAM record: %w", err)
	}
	return nil
}
