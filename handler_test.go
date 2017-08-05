package admissionwebhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"k8s.io/api/admission/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apiserver/pkg/admission"
)

type FakeAdmissionPlugin struct{}

func (fap *FakeAdmissionPlugin) Admit(a admission.Attributes) error {
	return nil
}

func (fap *FakeAdmissionPlugin) Handles(a admission.Operation) bool {
	return true
}

type FakeMutatingAdmissionPlugin struct{}

func (fap *FakeMutatingAdmissionPlugin) Admit(a admission.Attributes) error {
	obj := a.GetObject()
	pod, _ := obj.(*v1.Pod)
	pod.Spec.NodeName = "node1"
	return nil
}

func (fap *FakeMutatingAdmissionPlugin) Handles(a admission.Operation) bool {
	return true
}

type PodDecoder struct{}

func (pd *PodDecoder) Decode(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	target := v1.Pod{}
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, nil, err
	}
	if target.Kind != "Pod" {
		return nil, nil, fmt.Errorf("decoded object is not a pod, got %s. Data: %v", target.Kind, data)
	}
	return &target, nil, nil
}

func TestServeHTTP(t *testing.T) {
	goodTypeMeta := metav1.TypeMeta{
		Kind:       "AdmissionReview",
		APIVersion: "admissionregistration.k8s.io/v1alpha1",
	}
	badKindTypeMeta := metav1.TypeMeta{
		Kind:       "BadKind",
		APIVersion: "admissionregistration.k8s.io/v1alpha1",
	}
	badAPIVersionTypeMeta := metav1.TypeMeta{
		Kind:       "AdmissionReview",
		APIVersion: "bad.meta/reject",
	}
	kind := metav1.GroupVersionKind{
		Group:   "gvkGroup",
		Kind:    "gvkKind",
		Version: "gvkVersion",
	}
	resource := metav1.GroupVersionResource{
		Group:    "gvrGroup",
		Resource: "gvrResource",
		Version:  "gvrVersion",
	}
	userInfo := authenticationv1.UserInfo{
		Extra:    make(map[string]authenticationv1.ExtraValue),
		Groups:   []string{"group1"},
		UID:      "500",
		Username: "auser",
	}
	goodARSpec := v1alpha1.AdmissionReviewSpec{
		Name:        "ARSTest",
		Namespace:   "namespace1",
		Resource:    resource,
		SubResource: "subresource",
		Operation:   "CREATE",
		Object: runtime.RawExtension{
			Object: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod0",
					Namespace: "namespace1",
				},
				Spec: v1.PodSpec{
					NodeName: "node0",
				},
			},
		},
		OldObject: runtime.RawExtension{
			Object: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod0",
					Namespace: "namespace1",
				},
				Spec: v1.PodSpec{
					NodeName: "node0",
				},
			},
		},
		Kind:     kind,
		UserInfo: userInfo,
	}

	noOldObjARSpec := v1alpha1.AdmissionReviewSpec{
		Name:        "ARSTest",
		Namespace:   "namespace1",
		Resource:    resource,
		SubResource: "subresource",
		Operation:   "CREATE",
		Object: runtime.RawExtension{
			Object: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod0",
					Namespace: "namespace1",
				},
				Spec: v1.PodSpec{
					NodeName: "node0",
				},
			},
		},
		Kind:     kind,
		UserInfo: userInfo,
	}

	badObjARSpec := v1alpha1.AdmissionReviewSpec{
		Name:        "ARSTest",
		Namespace:   "namespace1",
		Resource:    resource,
		SubResource: "subresource",
		Operation:   "CREATE",
		Object: runtime.RawExtension{
			Object: &v1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind: "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service",
					Namespace: "namespace1",
				},
			},
		},
		OldObject: runtime.RawExtension{
			Object: &v1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind: "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service",
					Namespace: "namespace1",
				},
			},
		},
		Kind:     kind,
		UserInfo: userInfo,
	}

	testCases := map[string]struct {
		Obj        interface{}
		ShouldPass bool
		Decoder    runtime.Decoder
		Plugin     admission.Interface
	}{
		"valid AdmissionReview": {
			Obj:        v1alpha1.AdmissionReview{TypeMeta: goodTypeMeta, Spec: goodARSpec},
			ShouldPass: true,
			Decoder:    &PodDecoder{},
			Plugin:     &FakeAdmissionPlugin{},
		},
		"no OldObject in AdmissionReview": {
			Obj:        v1alpha1.AdmissionReview{TypeMeta: goodTypeMeta, Spec: noOldObjARSpec},
			ShouldPass: true,
			Decoder:    &PodDecoder{},
			Plugin:     &FakeAdmissionPlugin{},
		},
		"invalid kind in TypeMeta": {
			Obj:        v1alpha1.AdmissionReview{TypeMeta: badKindTypeMeta, Spec: goodARSpec},
			ShouldPass: false,
			Decoder:    &PodDecoder{},
			Plugin:     &FakeAdmissionPlugin{},
		},
		"invalid APIVersion in TypeMeta": {
			Obj:        v1alpha1.AdmissionReview{TypeMeta: badAPIVersionTypeMeta, Spec: goodARSpec},
			ShouldPass: false,
			Decoder:    &PodDecoder{},
			Plugin:     &FakeAdmissionPlugin{},
		},
		"mutating admission plugin": {
			Obj:        v1alpha1.AdmissionReview{TypeMeta: goodTypeMeta, Spec: goodARSpec},
			ShouldPass: false,
			Decoder:    &PodDecoder{},
			Plugin:     &FakeMutatingAdmissionPlugin{},
		},
		"bad object": {
			Obj:        v1alpha1.AdmissionReview{TypeMeta: goodTypeMeta, Spec: badObjARSpec},
			ShouldPass: false,
			Decoder:    &PodDecoder{},
			Plugin:     &FakeAdmissionPlugin{},
		},
	}

	for k, tc := range testCases {
		enc, _ := json.Marshal(tc.Obj)
		req := httptest.NewRequest("GET", "/", bytes.NewReader(enc))
		w := httptest.NewRecorder()
		handler := NewAdmissionPluginHandler(tc.Plugin, tc.Decoder)
		handler.ServeHTTP(w, req)
		resp := w.Result()

		new := &v1alpha1.AdmissionReview{}
		if err := json.NewDecoder(resp.Body).Decode(new); err != nil {
			t.Fatal(err)
		}
		if new.Status.Allowed != tc.ShouldPass {
			t.Errorf("%s: allowed was %t but should have been %t: %v", k, new.Status.Allowed, tc.ShouldPass, new)
		}

		t.Logf("%s: %v", k, new.Status)
		if new.Status.Allowed == false && new.Status.Result == nil {
			t.Errorf("%s: status error was nil", k)
		}
	}
}
