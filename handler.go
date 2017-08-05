package admissionwebhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

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
	Decoder         runtime.Decoder
}

// NewAdmissionPluginHandler returns a new handler that wraps an admission plugin
func NewAdmissionPluginHandler(plugin admission.Interface, decoder runtime.Decoder) http.Handler {
	return &AdmissionPluginHandler{AdmissionPlugin: plugin, Decoder: decoder}
}

func (aph *AdmissionPluginHandler) createAdmissionRecord(ar v1alpha1.AdmissionReviewSpec) (admission.Attributes, error) {
	var oldObj runtime.Object

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

	obj, _, err := aph.Decoder.Decode(ar.Object.Raw, &kind, nil)
	if err != nil {
		return nil, err
	}

	// Until an unmarshalling bug is fixed, need to do a different comparision
	// See: https://github.com/kubernetes/kubernetes/pull/50329
	//	if len(ar.OldObject.Raw) > 0 {
	if !bytes.Equal(ar.OldObject.Raw, []byte("null")) {
		oldObj, _, err = aph.Decoder.Decode(ar.OldObject.Raw, &kind, nil)
		if err != nil {
			return nil, err
		}
	}

	attrs := admission.NewAttributesRecord(obj, oldObj, kind, ar.Namespace, ar.Name, resource, ar.SubResource, ar.Operation, &userInfo)
	return attrs, nil
}

// ServeHTTP handles HTTP requests from a web server
func (aph *AdmissionPluginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ar := &v1alpha1.AdmissionReview{}
	allowed := false

	if err := json.NewDecoder(r.Body).Decode(ar); err != nil {
		aph.sendResponse(w, ar, allowed, err)
		return
	}

	if ar.Kind != "AdmissionReview" || ar.APIVersion != "admissionregistration.k8s.io/v1alpha1" {
		aph.sendResponse(w, ar, allowed, fmt.Errorf("unknown kind/version %q/%q", ar.Kind, ar.APIVersion))
		return
	}

	admission, err := aph.createAdmissionRecord(ar.Spec)
	if err != nil {
		aph.sendResponse(w, ar, allowed, fmt.Errorf("failed to convert to admission record: %v", err))
		return
	}
	origObj := admission.GetObject().DeepCopyObject()
	err = aph.AdmissionPlugin.Admit(admission)
	if !reflect.DeepEqual(origObj, admission.GetObject()) {
		aph.sendResponse(w, ar, allowed, fmt.Errorf("admission plugin wants to mutate the object, but mutation is not supported"))
		return
	}
	if err == nil {
		allowed = true
	}

	// TODO: encode mutated objects

	aph.sendResponse(w, ar, allowed, err)
}

func (aph *AdmissionPluginHandler) sendResponse(w http.ResponseWriter, ar *v1alpha1.AdmissionReview, allowed bool, err error) {
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
