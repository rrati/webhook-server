package admissionpluginwebserver

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	//"io/ioutil"
	"net/http"

	"k8s.io/client-go/kubernetes"
	//"k8s.io/client-go/kubernetes/typed/admissionregistration/v1alpha1"
	//"k8s.io/client-go/kubernetes/typed/core/v1"
	//	"k8s.io/client-go/rest"

	"k8s.io/api/admission/v1alpha1"
	//arv1alpha1 "k8s.io/api/admissionregistration/v1alpha1"
	//"k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"

	"github.com/golang/glog"
)

type AdmissionPluginWebServerConfig struct {
	ServerIP        string
	ServerPort      int
	ServerCertFile  string
	ServerKeyFile   string
	AdmissionPlugin admission.Interface
	Decoder         func([]byte) (runtime.Object, error)
}

type AdmissionPluginWebServer struct {
	AdmissionPluginWebServerConfig

	server     *http.Server
	kubeClient *kubernetes.Clientset
}

func NewAdmissionPluginWebServer(config AdmissionPluginWebServerConfig) AdmissionPluginWebServer {
	apws := AdmissionPluginWebServer{
		AdmissionPluginWebServerConfig: config,
		kubeClient:                     kubeClient(),
	}
	apws.configWebServer()

	return apws
}

func (apws *AdmissionPluginWebServer) configWebServer() {
	http.HandleFunc("/", apws.Serve)
	apws.server = &http.Server{Addr: fmt.Sprintf("%s:%d", apws.ServerIP, apws.ServerPort)}
	apws.configTLS()
	return
}

func (apws *AdmissionPluginWebServer) configTLS() {
	apiserverCert := apws.getAPIServerCert()
	apiserverCa := x509.NewCertPool()
	apiserverCa.AppendCertsFromPEM(apiserverCert)

	serverCert, err := tls.LoadX509KeyPair(apws.ServerCertFile, apws.ServerCertFile)
	if err != nil {
		glog.Fatalf("failed to load x509 key pair: %v", err)
	}

	apws.server.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    apiserverCa,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	return
}

func (apws *AdmissionPluginWebServer) getAPIServerCert() []byte {
	cm, err := apws.kubeClient.CoreV1().ConfigMaps("kube-system").Get("extension-apiserver-authentication", metav1.GetOptions{})
	if err != nil {
		glog.Fatalf("failed to get apiserver certificate: %v", err)
	}

	apiserverPem, ok := cm.Data["requestheader-client-ca-file"]
	if !ok {
		glog.Fatalf("failed to find ca certificare in the configmap data: %v", cm.Data)
	}
	return []byte(apiserverPem)
}

func (apws *AdmissionPluginWebServer) createAdmissionRecord(ar v1alpha1.AdmissionReviewSpec) (admission.Attributes, error) {
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

	obj, err := apws.Decoder(ar.Object.Raw)
	if err != nil {
		return nil, err
	}
	oldObj, err := apws.Decoder(ar.OldObject.Raw)
	if err != nil {
		return nil, err
	}

	attrs := admission.NewAttributesRecord(obj, oldObj, kind, ar.Namespace, ar.Name, resource, ar.SubResource, ar.Operation, &userInfo)
	return attrs, nil
}

func (apws *AdmissionPluginWebServer) Serve(w http.ResponseWriter, r *http.Request) {
	ar := v1alpha1.AdmissionReview{}
	allowed := true

	if err := json.NewDecoder(r.Body).Decode(ar); err != nil {
		apws.sendResponse(w, ar, allowed, err)
		return
	}

	if ar.Kind != "AdmissionReview" {
		apws.sendResponse(w, ar, allowed, fmt.Errorf("unknown kind %q %q", ar.Kind))
		return
	}

	admission, err := apws.createAdmissionRecord(ar.Spec)
	if err != nil {
		apws.sendResponse(w, ar, allowed, fmt.Errorf("failed to convert to admission record: %v", err))
	}
	err = apws.AdmissionPlugin.Admit(admission)
	if err != nil {
		allowed = false
	}

	// TODO: encode mutated objects

	apws.sendResponse(w, ar, allowed, err)
}

func (apws *AdmissionPluginWebServer) sendResponse(w http.ResponseWriter, ar v1alpha1.AdmissionReview, allowed bool, err error) {
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
