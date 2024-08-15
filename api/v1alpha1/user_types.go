package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UserSpec defines the desired state of User
type UserSpec struct {
	// SQLTemplate defines templates to configure SQL objects.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SQLTemplate `json:",inline"`
	// MariaDBRef is a reference to a MariaDB object.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	MariaDBRef MariaDBRef `json:"mariaDbRef" webhook:"inmutable"`
	// PasswordSecretKeyRef is a reference to the password to be used by the User.
	// If not provided, the account will be locked and the password will expire.
	// If the referred Secret is labeled with "k8s.mariadb.com/watch", updates may be performed to the Secret in order to update the password.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PasswordSecretKeyRef *corev1.SecretKeySelector `json:"passwordSecretKeyRef,omitempty"`
	// MaxUserConnections defines the maximum number of connections that the User can establish.
	// +optional
	// +kubebuilder:default=10
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MaxUserConnections int32 `json:"maxUserConnections,omitempty"` //webhook:"inmutable"`
	// Name overrides the default name provided by metadata.name.
	// +optional
	// +kubebuilder:validation:MaxLength=80
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Name string `json:"name,omitempty" webhook:"inmutable"`
	// Host related to the User.
	// +optional
	// +kubebuilder:validation:MaxLength=255
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Host string `json:"host,omitempty" webhook:"inmutable"`
}

// UserStatus defines the observed state of User
type UserStatus struct {
	// Conditions for the User object.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors={"urn:alm:descriptor:io.kubernetes.conditions"}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (u *UserStatus) SetCondition(condition metav1.Condition) {
	if u.Conditions == nil {
		u.Conditions = make([]metav1.Condition, 0)
	}
	meta.SetStatusCondition(&u.Conditions, condition)
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=umdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message"
// +kubebuilder:printcolumn:name="MaxConns",type="string",JSONPath=".spec.maxUserConnections"
// +kubebuilder:printcolumn:name="MariaDB",type="string",JSONPath=".spec.mariaDbRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{User,v1alpha1}}

// User is the Schema for the users API.  It is used to define grants as if you were running a 'CREATE USER' statement.
type User struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UserSpec   `json:"spec,omitempty"`
	Status UserStatus `json:"status,omitempty"`
}

func (u *User) AccountName() string {
	return fmt.Sprintf("'%s'@'%s'", u.UsernameOrDefault(), u.HostnameOrDefault())
}

func (u *User) UsernameOrDefault() string {
	if u.Spec.Name != "" {
		return u.Spec.Name
	}
	return u.Name
}

func (u *User) HostnameOrDefault() string {
	if u.Spec.Host != "" {
		return u.Spec.Host
	}
	return "%"
}

func (u *User) IsBeingDeleted() bool {
	return !u.DeletionTimestamp.IsZero()
}

func (u *User) IsReady() bool {
	return meta.IsStatusConditionTrue(u.Status.Conditions, ConditionTypeReady)
}

func (u *User) MariaDBRef() *MariaDBRef {
	return &u.Spec.MariaDBRef
}

func (d *User) RequeueInterval() *metav1.Duration {
	return d.Spec.RequeueInterval
}

func (u *User) RetryInterval() *metav1.Duration {
	return u.Spec.RetryInterval
}

// UserPasswordSecretFieldPath is the path related to the password Secret field.
const UserPasswordSecretFieldPath = ".spec.passwordSecretKeyRef.name"

// IndexerFuncForFieldPath returns an indexer function for a given field path.
func (m *User) IndexerFuncForFieldPath(fieldPath string) (client.IndexerFunc, error) {
	switch fieldPath {
	case UserPasswordSecretFieldPath:
		return func(obj client.Object) []string {
			user, ok := obj.(*User)
			if !ok {
				return nil
			}
			if user.Spec.PasswordSecretKeyRef != nil && user.Spec.PasswordSecretKeyRef.LocalObjectReference.Name != "" {
				return []string{user.Spec.PasswordSecretKeyRef.LocalObjectReference.Name}
			}
			return nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported field path: %s", fieldPath)
	}
}

// +kubebuilder:object:root=true

// UserList contains a list of User
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []User `json:"items"`
}

// ListItems gets a copy of the Items slice.
func (m *UserList) ListItems() []client.Object {
	items := make([]client.Object, len(m.Items))
	for i, item := range m.Items {
		items[i] = item.DeepCopy()
	}
	return items
}

func init() {
	SchemeBuilder.Register(&User{}, &UserList{})
}
