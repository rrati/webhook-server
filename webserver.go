package admissionwebhook

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
)

// AdmissionPluginWebServerConfig defines configuration parameters needed to run
// an web server that will serve the provided admission plugin
type AdmissionPluginWebServerConfig struct {
	AdmissionPluginHandler

	ServerIP       string
	ServerPort     int
	ServerCertFile string
	ServerKeyFile  string
	kubeClient     *kubernetes.Clientset
}

// AdmissionPluginWebServer wraps and serves a provided admission plugin as a Webhook
type AdmissionPluginWebServer struct {
	AdmissionPluginWebServerConfig

	server  *http.Server
	handler http.Handler
}

// NewAdmissionPluginWebServer will return an AdmissionPluginWebServer that wrapps an admission plugin
func NewAdmissionPluginWebServer(config AdmissionPluginWebServerConfig) AdmissionPluginWebServer {
	apws := AdmissionPluginWebServer{
		AdmissionPluginWebServerConfig: config,
		handler: NewAdmissionPluginHandler(config.AdmissionPlugin, config.Decoder),
	}
	apws.configWebServer()

	return apws
}

func (apws *AdmissionPluginWebServer) configWebServer() {
	http.HandleFunc("/", apws.handler.ServeHTTP)
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

// Start will start the web server
func (apws *AdmissionPluginWebServer) Start() {
	apws.server.ListenAndServeTLS("", "")
}
