package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

const DEVICE_VID = "B00B"
const DEVICE_PID = "6969"

type PVEInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Node        string `json:"node"`
	IPAddr      string
	Id          string  `json:"id"`
	Status      string  `json:"status"`
	MaxCpu      int     `json:"maxcpu"`
	MaxDisk     int     `json:"maxdisk"`
	MaxRAM      int     `json:"maxmem"`
	CurrentRam  float32 `json:"mem"`
	CurrentDisk float32 `json:"disk"`
	CurrentCpu  float32 `json:"cpu"`
}

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

type ListItem struct {
	MaxDisk     float32
	MaxRAM      float32
	CurrentRam  float32
	CurrentDisk float32
	CurrentCpu  float32
}

func initSerial() (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		Parity:   0,
		DataBits: 8,
		StopBits: 1,
	}

	// Get all serial ports
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		fmt.Println("Error listing serial ports:", err)
		return nil, err
	}

	// If no ports found
	if len(ports) == 0 {
		fmt.Println("No serial ports found")
		return nil, err
	}

	// Find the port with the specified VID and PID
	for _, port := range ports {
		if port.IsUSB {
			if port.VID == DEVICE_VID && port.PID == DEVICE_PID {

				// Try opening the serial port
				serialPort, err := serial.Open(port.SerialNumber, mode)
				if err != nil {
					fmt.Println("Error opening serial port", err)
				}

				return serialPort, nil // return the port to the parent
			}
		}
	}

	return nil, err

}

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
		var ip string
		switch vms[i].Type {
		case "lxc":
			ip, err = getLXCIP(vms[i])
		case "qemu":
			ip, err = getQEMUIP(vms[i])
		default:
			fmt.Printf("Skipping IP lookup for unsupported Proxmox guest type %q (VM %s)\n", vms[i].Type, vms[i].Id)
			continue
		}
		if err != nil {
			fmt.Printf("Unable to get IP for %s/%s: %v\n", vms[i].Type, vms[i].Id, err)
			continue
		}
		vms[i].IPAddr = ip
	}

	return vms, nil
}

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

	return "", fmt.Errorf("no IPv4 address found for QEMU VM %d", vm.Id)
}

func main() {
	fmt.Println("ProxStat v0.01")
	fmt.Printf("Connecting to device with VID: %s, PID: %s \n", DEVICE_VID, DEVICE_PID)

	//var serialPort, err = initSerial()
	// if err != nil {
	// return
	// }

	// Get the host's info
	hostInfo, err := getProxmoxHostInfo()
	if err != nil {
		fmt.Println("Error getting Proxmox host info:", err)
	}
	fmt.Println(hostInfo)

	vmInfo, err := getProxmoxVMs()
	if err != nil {
		fmt.Println("Error getting Proxmox VMs info:", err)
	}
	if err == nil {
		fmt.Println(vmInfo)
	}
	// H|n=Proxmox:|ip=192.168.1.254|d=412/931|r=27/64|b=86

	headerHost := hostInfo[0]
	hostCpuUsage := int(headerHost.CurrentCpu * 100)
	headerInfo := fmt.Sprintf(
		"H|n=%s:|ip=%s|d=%d/%d|r=%d/%d|b=%d",
		headerHost.Name,
		headerHost.IPAddr,
		(int(headerHost.CurrentDisk) / 1000000),
		(int(headerHost.MaxDisk) / 1000000),
		(int(headerHost.CurrentRam) / 1000000),
		(int(headerHost.MaxRAM) / 1000000),
		hostCpuUsage,
	)

	fmt.Println(headerInfo)
	for idx, li := range vmInfo {
		cpuUsage := int(li.CurrentCpu * 100)
		listItemString := fmt.Sprintf(
			"%d|n=%s:|ip=%s|d=%d/%d|r=%d/%d|b=%d",
			idx,
			li.Name,
			li.IPAddr,
			(int(li.CurrentDisk) / 1000000),
			(int(li.MaxDisk) / 1000000),
			(int(li.CurrentRam) / 1000000),
			(int(li.MaxRAM) / 1000000),
			cpuUsage,
		)
		fmt.Println(listItemString)
	}

	//serialPort.Write()

}
