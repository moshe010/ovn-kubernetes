package cni

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/pkg/types/current"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

var minRsrc = resource.MustParse("1k")
var maxRsrc = resource.MustParse("1P")

func validateBandwidthIsReasonable(rsrc *resource.Quantity) error {
	if rsrc.Value() < minRsrc.Value() {
		return fmt.Errorf("resource is unreasonably small (< 1kbit)")
	}
	if rsrc.Value() > maxRsrc.Value() {
		return fmt.Errorf("resoruce is unreasonably large (> 1Pbit)")
	}
	return nil
}

func extractPodBandwidthResources(podAnnotations map[string]string) (int64, int64, error) {
	ingress := int64(-1)
	egress := int64(-1)
	str, found := podAnnotations["kubernetes.io/ingress-bandwidth"]
	if found {
		ingressVal, err := resource.ParseQuantity(str)
		if err != nil {
			return -1, -1, err
		}
		if err := validateBandwidthIsReasonable(&ingressVal); err != nil {
			return -1, -1, err
		}
		ingress = ingressVal.Value()
	}
	str, found = podAnnotations["kubernetes.io/egress-bandwidth"]
	if found {
		egressVal, err := resource.ParseQuantity(str)
		if err != nil {
			return -1, -1, err
		}
		if err := validateBandwidthIsReasonable(&egressVal); err != nil {
			return -1, -1, err
		}
		egress = egressVal.Value()
	}
	return ingress, egress, nil
}

func isNotFoundError(err error) bool {
	statusErr, ok := err.(*errors.StatusError)
	return ok && statusErr.Status().Code == http.StatusNotFound
}

func podDescription(pr *PodRequest) string {
	return fmt.Sprintf("[%s/%s]", pr.PodNamespace, pr.PodName)
}

func (pr *PodRequest) cmdAdd() ([]byte, error) {
	namespace := pr.PodNamespace
	podName := pr.PodName
	if namespace == "" || podName == "" {
		return nil, fmt.Errorf("required CNI variable missing")
	}

	annotations, err := GetPodAnnotations(namespace, podName, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod annotation: %v", err)
	}
	podInfo, err := util.UnmarshalPodAnnotation(annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ovn annotation: %v", err)
	}

	ingress, egress, err := extractPodBandwidthResources(annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bandwidth request: %v", err)
	}
	podInterfaceInfo := &PodInterfaceInfo{
		PodAnnotation: *podInfo,
		MTU:           config.Default.MTU,
		Ingress:       ingress,
		Egress:        egress,
	}
	response := &Response{}
	if !config.UnprivilegedMode {
		response.Result, err = pr.getCNIResult(podInterfaceInfo)
		if err != nil {
			return nil, err
		}
	} else {
		response.PodIFInfo = podInterfaceInfo
	}

	responseBytes, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pod request response: %v", err)
	}

	return responseBytes, nil
}

func (pr *PodRequest) cmdDel() ([]byte, error) {
	if err := pr.PlatformSpecificCleanup(); err != nil {
		return nil, err
	}
	return []byte{}, nil
}

// HandleCNIRequest is the callback for all the requests
// coming to the cniserver after being procesed into PodRequest objects
// Argument '*PodRequest' encapsulates all the necessary information
// Return value is the actual bytes to be sent back without further processing.
func HandleCNIRequest(request *PodRequest) ([]byte, error) {
	pd := podDescription(request)
	logrus.Infof("%s dispatching pod network request %v", pd, request)
	var result []byte
	var err error
	switch request.Command {
	case CNIAdd:
		result, err = request.cmdAdd()
	case CNIDel:
		result, err = request.cmdDel()
	default:
	}
	logrus.Infof("%s CNI request %v, result %q, err %v", pd, request, string(result), err)
	if err != nil {
		// Prefix errors with pod info for easier failure debugging
		return nil, fmt.Errorf("%s %v", pd, err)
	}
	return result, nil
}

// getCNIResult get result from pod interface info.
func (pr *PodRequest) getCNIResult(podInterfaceInfo *PodInterfaceInfo) (*current.Result, error) {
	interfacesArray, err := pr.ConfigureInterface(pr.PodNamespace, pr.PodName, podInterfaceInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to configure pod interface: %v", err)
	}

	// Build the result structure to pass back to the runtime
	ipVersion := "6"
	if podInterfaceInfo.IP.IP.To4() != nil {
		ipVersion = "4"
	}
	return &current.Result{
		Interfaces: interfacesArray,
		IPs: []*current.IPConfig{
			{
				Version:   ipVersion,
				Interface: current.Int(1),
				Address:   *podInterfaceInfo.IP,
				Gateway:   podInterfaceInfo.GW,
			},
		},
	}, nil
}
