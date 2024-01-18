/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MariaBackupStorage struct {
	Volume                *corev1.VolumeSource              `json:"volume,omitempty"`
	PersistentVolumeClaim *corev1.PersistentVolumeClaimSpec `json:"persistentVolumeClaim,omitempty"`
}

func (s *MariaBackupStorage) Validate() error {
	if s.Volume == nil && s.PersistentVolumeClaim == nil {
		return errors.New("no storage type provided")
	}
	return nil
}

// BackupSpec defines the desired state of Backup
type MariaBackupSpec struct {
	// +kubebuilder:validation:Required
	MariaDBRef MariaDBRef `json:"mariaDbRef" webhook:"inmutable"`
	// +kubebuilder:validation:Required
	Storage MariaBackupStorage `json:"storage" webhook:"inmutable"`

	Args []string `json:"args,omitempty"`

	Schedule *Schedule `json:"schedule,omitempty"`
	// +kubebuilder:default=30
	MaxRetentionDays int32 `json:"maxRetentionDays,omitempty" webhook:"inmutable"`
	// +kubebuilder:default=5
	BackoffLimit int32 `json:"backoffLimit,omitempty"`
	// +kubebuilder:default=OnFailure
	RestartPolicy corev1.RestartPolicy `json:"restartPolicy,omitempty" webhook:"inmutable"`
	// +kubebuilder:validation:Optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty" webhook:"inmutable"`

	Affinity     *corev1.Affinity    `json:"affinity,omitempty"`
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
}

// BackupStatus defines the observed state of Backup
type MariaBackupStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (b *MariaBackupStatus) SetCondition(condition metav1.Condition) {
	if b.Conditions == nil {
		b.Conditions = make([]metav1.Condition, 0)
	}
	meta.SetStatusCondition(&b.Conditions, condition)
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=mbmdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Complete",type="string",JSONPath=".status.conditions[?(@.type==\"Complete\")].status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Complete\")].message"
// +kubebuilder:printcolumn:name="MariaDB",type="string",JSONPath=".spec.mariaDbRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MariaBackup is the Schema for the mariabackups API
type MariaBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MariaBackupSpec   `json:"spec,omitempty"`
	Status MariaBackupStatus `json:"status,omitempty"`
}

func (b *MariaBackup) IsComplete() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, ConditionTypeComplete)
}

func (b *MariaBackup) Volume() (*corev1.VolumeSource, error) {
	if b.Spec.Storage.Volume != nil {
		return b.Spec.Storage.Volume, nil
	}
	if b.Spec.Storage.PersistentVolumeClaim != nil {
		return &corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: b.Name,
			},
		}, nil
	}
	return nil, errors.New("unable to get volume from Backup")
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup
type MariaBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MariaBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MariaBackup{}, &MariaBackupList{})
}
