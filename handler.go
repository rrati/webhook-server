package admissionwebhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/api/admission/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
)

// AdmissionPluginHandler is a handler for wrapping an admission plugin
type AdmissionPluginHandler struct {
	AdmissionPlugin admission.Interface
	Decoder         func([]byte) (runtime.Object, error)
}

// NewAdmissionPluginHandler returns a new handler that wraps an admission plugin
func NewAdmissionPluginHandler(plugin admission.Interface, decoder func([]byte) (runtime.Object, error)) http.Handler {
	return &AdmissionPluginHandler{AdmissionPlugin: plugin, Decoder: decoder}
}

func (aph *AdmissionPluginHandler) createAdmissionRecord(ar v1alpha1.AdmissionReviewSpec) (admission.Attributes, error) {
	kind := schema.GroupVersionKind{
		Group:   ar.Kind.Group,
		Kind:    ar.Kind.Kind,
		Version: ar.Kind.Version,
	}
	resource := schema.GroupVersionResource{
		Group:    ar.Resource.Group,
		Resource: ar.Resource.Resource,
		Version:  ar.Resource.Version,
	}
	userInfo := user.DefaultInfo{
		Extra:  make(map[string][]string),
		Groups: ar.UserInfo.Groups,
		UID:    ar.UserInfo.UID,
		Name:   ar.UserInfo.Username,
	}

	for key, val := range ar.UserInfo.Extra {
		userInfo.Extra[key] = val
	}

	obj, err := aph.Decoder(ar.Object.Raw)
	if err != nil {
		return nil, err
	}
	oldObj, err := aph.Decoder(ar.OldObject.Raw)
	if err != nil {
		return nil, err
	}

	attrs := admission.NewAttributesRecord(obj, oldObj, kind, ar.Namespace, ar.Name, resource, ar.SubResource, ar.Operation, &userInfo)
	return attrs, nil
}

// ServeHTTP handles HTTP requests from a web server
func (aph *AdmissionPluginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ar := v1alpha1.AdmissionReview{}
	allowed := true

	if err := json.NewDecoder(r.Body).Decode(ar); err != nil {
		aph.sendResponse(w, ar, allowed, err)
		return
	}

	if ar.Kind != "AdmissionReview" {
		aph.sendResponse(w, ar, allowed, fmt.Errorf("unknown kind %q %q", ar.Kind))
		return
	}

	admission, err := aph.createAdmissionRecord(ar.Spec)
	if err != nil {
		aph.sendResponse(w, ar, allowed, fmt.Errorf("failed to convert to admission record: %v", err))
	}
	err = aph.AdmissionPlugin.Admit(admission)
	if err != nil {
		allowed = false
	}

	// TODO: encode mutated objects

	aph.sendResponse(w, ar, allowed, err)
}

func (aph *AdmissionPluginHandler) sendResponse(w http.ResponseWriter, ar v1alpha1.AdmissionReview, allowed bool, err error) {
	status := v1alpha1.AdmissionReviewStatus{}
	status.Allowed = allowed
	if err != nil {
		status.Result = &metav1.Status{
			Reason: metav1.StatusReason(fmt.Sprintf("error from admission plugin: %v", err)),
		}
	}

	ar.Status = status

	json.NewEncoder(w).Encode(ar)
}
