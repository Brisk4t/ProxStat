// Package internal is the ProxStat driver: it reads Proxmox through pvesh, keeps the display in step
// over USB serial, sending only what changed, and runs the daemon the CLI talks to over a Unix socket.
package internal

import (
	"log"
	"time"
)

// Driver owns the serial connection. The daemon runs one for its whole life, the CLI runs one
// for a single command when no daemon is up.
type Driver struct {
	cfg Config
	dev *device
	// frameOnConnect sends the Proxmox state after every (re)connect, because a device that
	// reconnected has usually reset and lost everything
	frameOnConnect bool
	needRows       bool

	// What the display shows as far as the driver knows, so only changes get sent. nil means
	// unknown, after a (re)connect or a rejected line, and the next push sends everything.
	shownSettings *settings
	shownRows     *rows

	lastUpdate  time.Time
	lastChanges int // lines the last update sent
}

// NewDriver returns a driver that connects on its first push. With frameOnConnect it also sends the
// Proxmox state after every (re)connect.
func NewDriver(cfg Config, frameOnConnect bool) *Driver {
	return &Driver{cfg: cfg, frameOnConnect: frameOnConnect}
}

// setConfig switches to new settings, reconnecting on the next push when the device ids changed.
// Changed settings are sent by the next push.
func (d *Driver) setConfig(cfg Config) {
	if cfg.VID != d.cfg.VID || cfg.PID != d.cfg.PID {
		d.Close()
	}
	d.cfg = cfg
}

// Port is the serial port of the connected device, "" when not connected
func (d *Driver) Port() string {
	if d.dev == nil {
		return ""
	}
	return d.dev.name
}

// Close closes the serial port, if it is open, and forgets what the display shows
func (d *Driver) Close() {
	if d.dev != nil {
		d.dev.close()
		d.dev = nil
	}
	d.forget()
}

// forget drops the record of what the display shows, so the next push sends all of it
func (d *Driver) forget() {
	d.shownSettings = nil
	d.shownRows = nil
}

// Push sends the settings that changed, the rows that changed when an update is due, then lines,
// connecting first if needed. A broken connection is reopened and everything sent once more, so an
// unplugged and replugged device recovers on the next push.
func (d *Driver) Push(lines ...string) ([]string, error) {
	var fetched *rows
	var err error
	for range 2 {
		if d.dev == nil {
			if d.dev, err = openDevice(d.cfg.VID, d.cfg.PID); err != nil {
				return nil, err
			}
			log.Printf("Connected to %s", d.dev.name)
			d.needRows = d.needRows || d.frameOnConnect
		}
		if d.needRows && fetched == nil {
			r, err := buildRows()
			if err != nil {
				return nil, err
			}
			fetched = &r
		}
		want := settingsOf(d.cfg)
		changes := diffSettings(d.shownSettings, want)
		if d.needRows {
			changes = append(changes, diffRows(d.shownRows, *fetched)...)
		}
		var warnings []string
		if warnings, err = d.dev.send(append(changes, lines...)); err != nil {
			log.Printf("Lost device: %v", err)
			d.Close()
			continue
		}
		d.shownSettings = &want
		if d.needRows {
			d.shownRows = fetched
			d.needRows = false
			d.lastUpdate = time.Now()
			d.lastChanges = len(changes)
		}
		if len(warnings) > 0 {
			// A rejected line may have been applied in part, so the display's state is unknown
			d.forget()
		}
		return warnings, nil
	}
	return nil, err
}

// Refresh reads the current Proxmox state and sends whatever changed since the last update
func (d *Driver) Refresh() ([]string, error) {
	d.needRows = true
	return d.Push()
}
