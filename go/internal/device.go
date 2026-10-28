package internal

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// How long the firmware gets to answer a line, and to answer "ver" right after the port opens
const (
	replyWait     = 3 * time.Second
	handshakeWait = time.Second
)

var ErrNoDevice = errors.New("device not found")

// device is an open connection to the display
type device struct {
	port serial.Port
	name string // serial port, e.g. /dev/ttyACM0
	buf  []byte
}

// FindPort returns the name of the first USB serial port with this VID and PID
func FindPort(vid, pid string) (string, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return "", fmt.Errorf("list serial ports: %w", err)
	}
	for _, port := range ports {
		if port.IsUSB && strings.EqualFold(port.VID, vid) && strings.EqualFold(port.PID, pid) {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("%w (VID %s, PID %s)", ErrNoDevice, vid, pid)
}

// openDevice opens the port and waits for the firmware to answer "ver"
func openDevice(vid, pid string) (*device, error) {
	name, err := FindPort(vid, pid)
	if err != nil {
		return nil, err
	}
	port, err := serial.Open(name, &serial.Mode{BaudRate: 115200, DataBits: 8, StopBits: serial.OneStopBit})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	d := &device{port: port, name: name}

	// Some boards reset when the port opens, so give the firmware a few tries to come up
	for range 3 {
		_ = port.ResetInputBuffer()
		d.buf = nil
		if err = d.writeLine("ver"); err != nil {
			break
		}
		var reply string
		if reply, err = d.readReply(handshakeWait); err == nil {
			if strings.HasPrefix(reply, "VER ") {
				return d, nil
			}
			err = fmt.Errorf("unexpected reply %q", reply)
		}
	}
	port.Close()
	return nil, fmt.Errorf("handshake with %s: %w", name, err)
}

// close closes the serial port
func (d *device) close() {
	d.port.Close()
}

// writeLine sends one protocol line, adding the newline
func (d *device) writeLine(line string) error {
	_, err := d.port.Write([]byte(line + "\n"))
	return err
}

// readReply returns the next OK / ERR / VER line, skipping the firmware's debug output
func (d *device) readReply(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	chunk := make([]byte, 128)
	for {
		if i := bytes.IndexByte(d.buf, '\n'); i >= 0 {
			line := strings.TrimRight(string(d.buf[:i]), "\r")
			d.buf = d.buf[i+1:]
			if line == "OK" || strings.HasPrefix(line, "ERR ") || strings.HasPrefix(line, "VER ") {
				return line, nil
			}
			continue
		}
		left := time.Until(deadline)
		if left <= 0 {
			return "", errors.New("no reply from device")
		}
		if err := d.port.SetReadTimeout(left); err != nil {
			return "", err
		}
		n, err := d.port.Read(chunk)
		if err != nil {
			return "", err
		}
		d.buf = append(d.buf, chunk[:n]...)
	}
}

// send writes each line and waits for its reply. ERR replies come back as warnings; the error is
// only for a broken connection, after which the device has to be reopened.
func (d *device) send(lines []string) ([]string, error) {
	var warnings []string
	for _, line := range lines {
		if err := d.writeLine(line); err != nil {
			return warnings, fmt.Errorf("write to %s: %w", d.name, err)
		}
		reply, err := d.readReply(replyWait)
		if err != nil {
			return warnings, fmt.Errorf("%s: %w", d.name, err)
		}
		if strings.HasPrefix(reply, "ERR ") {
			warnings = append(warnings, fmt.Sprintf("%s: %s", line, reply))
		}
	}
	return warnings, nil
}
