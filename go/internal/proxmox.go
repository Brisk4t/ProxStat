package internal

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type PVEInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Node        string `json:"node"`
	IPAddr      string
	Id          string  `json:"id"`
	VMID        int     `json:"vmid"`
	Status      string  `json:"status"`
	MaxCpu      int     `json:"maxcpu"`
	MaxDisk     int     `json:"maxdisk"`
	MaxRAM      int     `json:"maxmem"`
	CurrentRam  float32 `json:"mem"`
	CurrentDisk float32 `json:"disk"`
	CurrentCpu  float32 `json:"cpu"`
}

// UnmarshalJSON decodes a cluster resource, using the node name as the name of a node
func (info *PVEInfo) UnmarshalJSON(data []byte) error {
	type pveInfo PVEInfo
	aux := struct {
		pveInfo
		Node string `json:"node"`
	}{}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*info = PVEInfo(aux.pveInfo)
	info.Node = aux.Node
	if info.Type == "node" {
		info.Name = aux.Node
	}

	return nil
}

// getProxmoxHostInfo returns every node in the cluster with its IP address.
// pvesh get /cluster/resources --type node --output-format json
func getProxmoxHostInfo() ([]PVEInfo, error) {
	cmd := exec.Command("pvesh", "get", "/cluster/resources", "--type", "node", "--output-format", "json")
	ipcmd := exec.Command("pvesh", "get", "/cluster/status", "--output-format", "json")

	resourceOutput, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get Proxmox host resources: %w", err)
	}

	statusOutput, err := ipcmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get Proxmox cluster status: %w", err)
	}

	// Array containing hosts (nodes)
	var hosts []PVEInfo
	if err := json.Unmarshal(resourceOutput, &hosts); err != nil {
		return nil, fmt.Errorf("decode Proxmox host resources: %w", err)
	}

	// The cluster status response is an array of node records.
	var clusterStatus []struct {
		Name   string `json:"name"`
		IPAddr string `json:"ip"`
	}
	if err := json.Unmarshal(statusOutput, &clusterStatus); err != nil {
		return nil, fmt.Errorf("decode Proxmox cluster status: %w", err)
	}

	// Name:IP map
	ipsByName := make(map[string]string, len(clusterStatus))
	for _, node := range clusterStatus {
		ipsByName[node.Name] = node.IPAddr
	}

	// Insert ip into the main PVEInfo struct
	for i := range hosts {
		hosts[i].IPAddr = ipsByName[hosts[i].Name]
	}

	return hosts, nil
}

// getProxmoxVMs returns every VM and container in the cluster, with IP addresses for running ones.
// pvesh get /cluster/resources --type vm --output-format json
func getProxmoxVMs() ([]PVEInfo, error) {
	cmd := exec.Command("pvesh", "get", "/cluster/resources", "--type", "vm", "--output-format", "json")

	cmdOutput, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("execute get VMs command: %w", err)
	}

	// Get VMs + LXCs
	var vms []PVEInfo
	if err := json.Unmarshal(cmdOutput, &vms); err != nil {
		return nil, fmt.Errorf("decode Proxmox VMs status: %w", err)
	}

	for i := range vms {
		// Stopped guests have no interfaces to ask about
		if vms[i].Status != "running" {
			continue
		}
		var ip string
		switch vms[i].Type {
		case "lxc":
			ip, err = getLXCIP(vms[i])
		case "qemu":
			ip, err = getQEMUIP(vms[i])
		default:
			log.Printf("Skipping IP lookup for unsupported Proxmox guest type %q (VM %s)", vms[i].Type, vms[i].Id)
			continue
		}
		if err != nil {
			log.Printf("Unable to get IP for %s/%s: %v", vms[i].Type, vms[i].Id, err)
			continue
		}
		vms[i].IPAddr = ip
	}

	return vms, nil
}

// getLXCIP returns the first IPv4 address of a container's non-loopback interfaces
func getLXCIP(vm PVEInfo) (string, error) {
	vmID := strings.TrimPrefix(vm.Id, "lxc/")
	cmd := exec.Command(
		"pvesh", "get",
		fmt.Sprintf("/nodes/%s/lxc/%s/interfaces", vm.Node, vmID),
		"--output-format", "json",
	)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get IP for LXC %s: %w", vm.Id, err)
	}

	var interfaces []struct {
		Name string `json:"name"`
		Inet string `json:"inet"`
	}
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return "", fmt.Errorf("decode interfaces for LXC %s: %w", vm.Id, err)
	}

	for _, iface := range interfaces {
		if iface.Name != "lo" && iface.Inet != "" {
			return strings.SplitN(iface.Inet, "/", 2)[0], nil
		}
	}

	return "", fmt.Errorf("no IPv4 address found for LXC %s", vm.Id)
}

// getQEMUIP returns the first IPv4 address the QEMU guest agent reports on a non-loopback interface
func getQEMUIP(vm PVEInfo) (string, error) {
	vmID := strings.TrimPrefix(vm.Id, "qemu/")
	cmd := exec.Command(
		"pvesh", "get",
		fmt.Sprintf("/nodes/%s/qemu/%s/agent/network-get-interfaces", vm.Node, vmID),
		"--output-format", "json",
	)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get IP for QEMU VM %s: %w", vm.Id, err)
	}

	var response struct {
		Result []struct {
			Name        string `json:"name"`
			IPAddresses []struct {
				AddressType string `json:"ip-address-type"`
				Address     string `json:"ip-address"`
			} `json:"ip-addresses"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("decode interfaces for QEMU VM %s: %w", vm.Id, err)
	}

	for _, iface := range response.Result {
		if iface.Name == "lo" {
			continue
		}
		for _, address := range iface.IPAddresses {
			if address.AddressType == "ipv4" && address.Address != "" {
				return address.Address, nil
			}
		}
	}

	return "", fmt.Errorf("no IPv4 address found for QEMU VM %s", vm.Id)
}
