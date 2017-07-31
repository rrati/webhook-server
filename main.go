package admissionpluginwebserver

import (
	//"crypto/tls"
	//"crypto/x509"
	//"encoding/json"
	//	"flag"
	//"fmt"
	//"net/http"

	"k8s.io/client-go/kubernetes"
	//"k8s.io/client-go/kubernetes/typed/admissionregistration/v1alpha1"
	//"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	"github.com/golang/glog"
)

func kubeClient() *kubernetes.Clientset {
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Error creating client: %v", err)
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Error creating client: %v", err)
	}
	return cs
}

func main() {
}
