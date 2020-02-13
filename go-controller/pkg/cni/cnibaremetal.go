package cni

// contains code for cnishim - one that gets called as the cni Plugin
// This does not do the real cni work. This is just the client to the cniserver
// that does the real work.

import (
	"fmt"
	"strconv"

	"github.com/Mellanox/sriovnet"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

// Plugin is the structure to hold the endpoint information and the corresponding
// functions to use it
type BaremetalPlugin struct {
	socketPath string
}

// NewCNIBaremetalPlugin creates the internal Plugin object
func NewCNIBaremetalPlugin(socketPath string) *BaremetalPlugin {
	if len(socketPath) == 0 {
		socketPath = serverSocketPath
	}
	return &BaremetalPlugin{socketPath: socketPath}
}

// CmdAdd is the callback for 'add' cni calls from skel
func (bp *BaremetalPlugin) CmdAdd(args *skel.CmdArgs) error {

	// read the config stdin args to obtain cniVersion
	conf, err := config.ReadCNIConfig(args.StdinData)
	if err != nil {
		return fmt.Errorf("invalid stdin args")
	}

	req := newCNIRequest(args)
	podReq, err := cniRequestToPodRequest(req)
	if err != nil {
		return fmt.Errorf("cniRequestToPodRequest failed %v", err)
	}
	isSmartNic := true
	if podReq.CNIConf.DeviceID == "" {
		return fmt.Errorf("DeviceID must be set")
	}
	pciAddress := podReq.CNIConf.DeviceID
	pfPciAddress, err := GetPfPciFromVfPci(pciAddress)
	if err != nil {
		return err
	}
	pfindex := pfPciAddress[len(pfPciAddress)-1]
	vfindex, err := sriovnet.GetVfIndexByPciAddress(pciAddress)
	if err != nil {
		return err
	}

	// 1. set smart-nic pod annotation will add PF index and VF index
	err = SetPodInfoSmartNic(podReq.PodNamespace, podReq.PodName, string(pfindex), strconv.Itoa(vfindex))
	if err != nil {
		return err
	}

	// 2. get POD annotation MAC/IP/GW/
	// TODO:check for error
	annotations, _ := GetPodAnnotations(podReq.PodNamespace, podReq.PodName, isSmartNic)
	podInfo, err := util.UnmarshalPodAnnotation(annotations)
	if err != nil {
		return fmt.Errorf("failed to unmarshal ovn annotation: %v", err)
	}
	//1. get VF netdevice from PCI
	podInterfaceInfo := &PodInterfaceInfo{
		PodAnnotation: *podInfo,
		MTU:           config.Default.MTU,
		IsSmartNic:    isSmartNic,
	}

	result, err := podReq.getCNIResult(podInterfaceInfo)
	if err != nil {
		return fmt.Errorf("failed to get CNI Result from pod interface info %v: %v", podInterfaceInfo, err)
	}

	return types.PrintResult(result, conf.CNIVersion)
}

// CmdDel is the callback for 'teardown' cni calls from skel
func (bp *BaremetalPlugin) CmdDel(args *skel.CmdArgs) error {
	return nil
}

// CmdCheck is the callback for 'checking' container's networking is as expected.
// Currently not implemented, so returns `nil`.
func (bp *BaremetalPlugin) CmdCheck(args *skel.CmdArgs) error {
	return nil
}
