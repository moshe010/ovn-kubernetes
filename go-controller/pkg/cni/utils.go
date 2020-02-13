package cni

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Mellanox/sriovnet"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/kube"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

func GetPodInfoSmartNIC(namespace string, podName string) (map[string]string, error) {
	envConfig := config.KubernetesConfig{
		APIServer:  "https://10.212.0.220:6443",
		Kubeconfig: "/etc/kubernetes/admin.conf",
	}

	clientset, err := util.NewClientset(&envConfig)
	if err != nil {
		logrus.Errorf("Could not create clientset for kubernetes: %v", err)
		return nil, err
	}
	kubecli := &kube.Kube{KClient: clientset}
	return kubecli.GetAnnotationsOnPod(namespace, podName)
}

func SetPodInfoSmartNic(namespace, podName, pfindex, vfindex string) error {
	envConfig := config.KubernetesConfig{
		APIServer:  "https://10.212.0.220:6443",
		Kubeconfig: "/etc/kubernetes/admin.conf",
	}
	clientset, err := util.NewClientset(&envConfig)
	if err != nil {
		logrus.Errorf("Could not create clientset for kubernetes: %v", err)
		return err
	}
	kubecli := &kube.Kube{KClient: clientset}
	//TODO add container id in anotation
	return kubecli.SetAnnotationsOnPod(namespace, podName, map[string]string{"ovn.smartnic.pf": pfindex, "ovn.smartnic.vf": vfindex})
}

func GetPodAnnotations(namespace string, podName string, isSmartNic bool) (annotations map[string]string, err error) {

	envConfig := config.KubernetesConfig{
		APIServer:  "https://10.212.0.220:6443",
		Kubeconfig: "/etc/kubernetes/admin.conf",
	}
	//clientset, err := util.NewClientset(&config.Kubernetes)
	clientset, err := util.NewClientset(&envConfig)
	if err != nil {
		return nil, fmt.Errorf("could not create kubernetes clientset: %v", err)
	}
	kubecli := &kube.Kube{KClient: clientset}

	// Get the IP address and MAC address from the API server.
	// Exponential back off ~32 seconds + 7* t(api call)
	var annotationBackoff = wait.Backoff{Duration: 1 * time.Second, Steps: 7, Factor: 1.5, Jitter: 0.1}
	if err = wait.ExponentialBackoff(annotationBackoff, func() (bool, error) {
		annotations, err = kubecli.GetAnnotationsOnPod(namespace, podName)
		if err != nil {
			if isNotFoundError(err) {
				// Pod not found; don't bother waiting longer
				return false, err
			}
			logrus.Warningf("error getting pod annotations: %v", err)
			return false, nil
		}
		if _, ok := annotations[util.OvnPodAnnotationName]; ok {
			if isSmartNic {
				if _, ok := annotations["vf_rep_ready"]; ok {
					return true, nil
				}
				return false, nil
			}
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to get pod annotation: %v", err)
	}

	return annotations, nil
}

//Move to sriovnet
func GetPfPciFromVfPci(vfPciAddress string) (string, error) {
	pfPath := filepath.Join(sriovnet.PciSysDir, vfPciAddress, "physfn")
	pciDevDir, err := os.Readlink(pfPath)
	if len(pciDevDir) <= 3 {
		return "", fmt.Errorf("could not find PCI Address")
	}
	return pciDevDir[3:], err
}
