package internal

import (
	"cmp"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Firmware limits, see the serial protocol in README.md
const (
	maxItems   = 12
	maxNameLen = 24
	maxIPLen   = 15
)

// The color commands, in the order of settings.colors, and the firmware's field keys, in the order
// of FieldColors.values
var (
	colorCommands = [3]string{"hfg", "lfg", "lbg"}
	colorFields   = [4]string{"n", "ip", "d", "r"}
)

// settings are the ones the device forgets on reset
type settings struct {
	rotation, perPage int
	colors            [3][4]string // header text, list text, list background; each by field
}

// entry is one header or list item as the display shows it, already formatted for the wire, so
// changes too small to show (a few MB of RAM) don't count as changes
type entry struct {
	name, ip  string
	disk, ram string // "used/total"
	bar       int    // percent
	online    bool   // list items only, the header has no online flag
}

// rows is everything below the settings: the host header, nil when there is none, and the guests
// in slot order
type rows struct {
	header *entry
	items  []entry
}

// settingsOf returns the settings for cfg
func settingsOf(cfg Config) settings {
	return settings{
		rotation: cfg.Rotation,
		perPage:  cfg.PerPage,
		colors:   [3][4]string{cfg.HeaderTextColor.values(), cfg.ListTextColor.values(), cfg.ListBackgroundColor.values()},
	}
}

// diffSettings returns a command for every setting that differs from prev. A nil prev means the
// device's settings are unknown, so all of them are sent.
func diffSettings(prev *settings, next settings) []string {
	var lines []string
	if prev == nil || prev.rotation != next.rotation {
		lines = append(lines, "rot="+strconv.Itoa(next.rotation))
	}
	if prev == nil || prev.perPage != next.perPage {
		lines = append(lines, "per_page="+strconv.Itoa(next.perPage))
	}
	for i, cmd := range colorCommands {
		for j, key := range colorFields {
			if prev == nil || prev.colors[i][j] != next.colors[i][j] {
				lines = append(lines, cmd+"="+key+","+next.colors[i][j])
			}
		}
	}
	return lines
}

// diffRows returns the lines that turn the display's rows from prev into next: only the changed
// fields of each row, and a del for each row that went away. A nil prev means the display's
// contents are unknown, so every row is sent whole and every unused slot deleted.
func diffRows(prev *rows, next rows) []string {
	var lines []string
	var oldHeader *entry
	if prev != nil {
		oldHeader = prev.header
	}
	switch {
	case next.header != nil:
		lines = appendEntry(lines, "H", next.header.fields(oldHeader, false))
	case prev == nil || oldHeader != nil:
		lines = append(lines, "H|del")
	}
	for i := range maxItems {
		target := strconv.Itoa(i + 1)
		var old *entry
		if prev != nil && i < len(prev.items) {
			old = &prev.items[i]
		}
		switch {
		case i < len(next.items):
			lines = appendEntry(lines, target, next.items[i].fields(old, true))
		case prev == nil || old != nil:
			lines = append(lines, target+"|del")
		}
	}
	return lines
}

// appendEntry adds the line setting fields on target, if there are any
func appendEntry(lines []string, target string, fields []string) []string {
	if len(fields) == 0 {
		return lines
	}
	return append(lines, target+"|"+strings.Join(fields, "|"))
}

// fields returns "key=value" for every field of e that differs from old, or all of them for a nil
// old (a row the display doesn't have yet)
func (e entry) fields(old *entry, withOnline bool) []string {
	var fields []string
	if old == nil || e.name != old.name {
		fields = append(fields, "n="+e.name)
	}
	if old == nil || e.ip != old.ip {
		fields = append(fields, "ip="+e.ip)
	}
	if old == nil || e.disk != old.disk {
		fields = append(fields, "d="+e.disk)
	}
	if old == nil || e.ram != old.ram {
		fields = append(fields, "r="+e.ram)
	}
	if old == nil || e.bar != old.bar {
		fields = append(fields, "b="+strconv.Itoa(e.bar))
	}
	if withOnline && (old == nil || e.online != old.online) {
		online := "0"
		if e.online {
			online = "1"
		}
		fields = append(fields, "on="+online)
	}
	return fields
}

// buildRows reads the current Proxmox state and returns the rows to show
func buildRows() (rows, error) {
	hosts, err := getProxmoxHostInfo()
	if err != nil {
		return rows{}, err
	}
	guests, err := getProxmoxVMs()
	if err != nil {
		return rows{}, err
	}

	var r rows
	if host, ok := localHost(hosts); ok {
		host.Name = clip(host.Name, maxNameLen-1) + ":"
		header := newEntry(host)
		r.header = &header
	}

	// Running guests first, then the rest grouped by status, each group by name. A guest that starts
	// or stops changes slots, so the cards it passes get redrawn.
	slices.SortFunc(guests, func(a, b PVEInfo) int {
		return cmp.Or(
			cmp.Compare(statusRank(a.Status), statusRank(b.Status)),
			strings.Compare(a.Status, b.Status),
			strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.VMID, b.VMID), // same name: keep the order stable between updates
		)
	})
	if len(guests) > maxItems {
		log.Printf("Showing the first %d of %d guests", maxItems, len(guests))
		guests = guests[:maxItems]
	}
	for _, guest := range guests {
		r.items = append(r.items, newEntry(guest))
	}
	return r, nil
}

// statusRank sorts running guests ahead of every other status
func statusRank(status string) int {
	if status == "running" {
		return 0
	}
	return 1
}

// localHost picks the node this runs on for the header, or the first node when it isn't one
func localHost(hosts []PVEInfo) (PVEInfo, bool) {
	if len(hosts) == 0 {
		return PVEInfo{}, false
	}
	if hostname, err := os.Hostname(); err == nil {
		for _, host := range hosts {
			if host.Name == hostname {
				return host, true
			}
		}
	}
	return hosts[0], true
}

// newEntry formats a node or guest for the display
func newEntry(info PVEInfo) entry {
	return entry{
		name:   clip(info.Name, maxNameLen),
		ip:     clip(info.IPAddr, maxIPLen),
		disk:   gb(float64(info.CurrentDisk)) + "/" + gb(float64(info.MaxDisk)),
		ram:    gb(float64(info.CurrentRam)) + "/" + gb(float64(info.MaxRAM)),
		bar:    int(info.CurrentCpu*100 + 0.5),
		online: info.Status == "running",
	}
}

// gb formats bytes in GiB (the firmware labels them GB): one decimal below 10, whole numbers above
func gb(bytes float64) string {
	v := bytes / (1 << 30)
	if v < 10 {
		return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// clip drops the field separator and cuts s to the firmware's field length
func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "|", "")
	if len(s) > n {
		s = s[:n]
	}
	return s
}
