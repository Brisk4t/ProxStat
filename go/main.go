package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"proxstat/internal"
)

const version = "0.02"

const usage = `ProxStat %s: shows Proxmox host and guest status on the ProxStat e-paper display

Usage:
  proxstat daemon                       start the background service that keeps the display up to date
  proxstat daemon --foreground          run it in this terminal instead, e.g. under systemd
  proxstat stop                         stop the background service
  proxstat refresh                      push the current state now
  proxstat next                         show the next page
  proxstat status                       show settings and the daemon's state
  proxstat set rotate <-1..3>           rotation in 90 degree steps clockwise
  proxstat set per-page <1..4>          list items per page
  proxstat set update-interval <min>    minutes between Proxmox polls
  proxstat set redraw-interval <hours>  hours between full refreshes against ghosting, 0 = off
  proxstat set vid <hex>                USB vendor id of the display
  proxstat set pid <hex>                USB product id of the display
  proxstat set header text-color <field> <color>
  proxstat set list text-color <field> <color>
  proxstat set list background-color <field> <color>
                                        field: name, ip, disk or ram; color: black, white, yellow or red

Settings are saved in %s.
Without a running daemon, refresh, next and set talk to the display directly.
`

// main runs the command line and exits with status 1 on any error
func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "proxstat:", err)
		os.Exit(1)
	}
}

// run dispatches the command line to its command
func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	switch cmd := args[0]; {
	case cmd == "daemon" && len(args) == 1:
		pid, logFile, err := internal.StartDaemon()
		if err != nil {
			return err
		}
		fmt.Printf("daemon started (pid %d), logging to %s\n", pid, logFile)
		return nil
	case cmd == "daemon" && len(args) == 2 && args[1] == "--foreground":
		log.SetFlags(log.LstdFlags)
		return internal.RunDaemon()
	case cmd == "stop" && len(args) == 1:
		if err := internal.StopDaemon(); err != nil {
			return err
		}
		fmt.Println("daemon stopped")
		return nil
	case (cmd == "refresh" || cmd == "next") && len(args) == 1:
		return runAction(cmd)
	case cmd == "status" && len(args) == 1:
		return runStatus()
	case cmd == "set" && (len(args) == 3 || len(args) == 5):
		return runSet(args[1:])
	case cmd == "help" || cmd == "-h" || cmd == "--help":
		printUsage()
		return nil
	}
	printUsage()
	return fmt.Errorf("bad arguments: %s", strings.Join(args, " "))
}

// printUsage prints the commands and where the settings are saved
func printUsage() {
	path, err := internal.ConfigPath()
	if err != nil {
		path = "the user config directory"
	}
	fmt.Printf(usage, version, path)
}

// runAction sends refresh or next through the daemon, or straight to the device without one
func runAction(cmd string) error {
	msg, err := internal.CallDaemon(cmd)
	if errors.Is(err, internal.ErrNoDaemon) {
		return direct(func(d *internal.Driver) ([]string, error) {
			if cmd == "refresh" {
				return d.Refresh()
			}
			return d.Push("next")
		})
	}
	return report(msg, err)
}

// runSet saves one setting ("<key> <value>" or "<scope> <kind> <field> <color>"), then has the
// daemon apply it, or sends the display's settings to it directly when no daemon is running
func runSet(args []string) error {
	cfg, err := internal.LoadConfig()
	if err != nil {
		return err
	}
	key := args[0]
	if len(args) == 2 {
		err = cfg.Set(key, args[1])
	} else {
		err = cfg.SetColor(args[0], args[1], args[2], args[3])
	}
	if err != nil {
		return err
	}
	if err := internal.SaveConfig(cfg); err != nil {
		return err
	}
	msg, err := internal.CallDaemon("reload")
	if !errors.Is(err, internal.ErrNoDaemon) {
		return report(msg, err)
	}
	if key != "rotate" && key != "per-page" && key != "header" && key != "list" {
		fmt.Println("saved")
		return nil
	}
	// The device forgets this on reset, so a missing device isn't a failure: the daemon sends it
	// again on the next connect
	err = direct(func(d *internal.Driver) ([]string, error) { return d.Push() })
	if errors.Is(err, internal.ErrNoDevice) {
		fmt.Println("saved; display not connected, it gets the setting on the next connect")
		return nil
	}
	return err
}

// direct runs one exchange with the device for when no daemon holds the port
func direct(f func(*internal.Driver) ([]string, error)) error {
	cfg, err := internal.LoadConfig()
	if err != nil {
		return err
	}
	d := internal.NewDriver(cfg, false)
	defer d.Close()
	warnings, err := f(d)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "device rejected", w)
	}
	fmt.Printf("done (%s, no daemon running)\n", d.Port())
	return nil
}

// report prints the daemon's message when the command worked
func report(msg string, err error) error {
	if err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}

// runStatus prints the saved settings and the daemon's state, or whether the device is plugged in
// when no daemon is running
func runStatus() error {
	path, err := internal.ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := internal.LoadConfig()
	if err != nil {
		return err
	}
	redraw := "off"
	if cfg.RedrawIntervalHours > 0 {
		redraw = fmt.Sprintf("%g h", cfg.RedrawIntervalHours)
	}
	fmt.Printf("config           %s\n", path)
	fmt.Printf("rotate           %d\n", cfg.Rotation)
	fmt.Printf("per-page         %d\n", cfg.PerPage)
	fmt.Printf("update-interval  %d min\n", cfg.UpdateIntervalMinutes)
	fmt.Printf("redraw-interval  %s\n", redraw)
	fmt.Printf("vid:pid          %s:%s\n", cfg.VID, cfg.PID)
	fmt.Printf("header text      %s\n", cfg.HeaderTextColor)
	fmt.Printf("list text        %s\n", cfg.ListTextColor)
	fmt.Printf("list background  %s\n", cfg.ListBackgroundColor)
	if logFile, err := internal.LogPath(); err == nil {
		fmt.Printf("log              %s\n", logFile)
	}

	msg, err := internal.CallDaemon("status")
	switch {
	case errors.Is(err, internal.ErrNoDaemon):
		found := "not found"
		if port, err := internal.FindPort(cfg.VID, cfg.PID); err == nil {
			found = "found at " + port
		}
		fmt.Printf("daemon           not running, display %s\n", found)
	case err != nil:
		fmt.Printf("daemon           %v\n", err)
	default:
		fmt.Printf("daemon           running, %s\n", msg)
	}
	return nil
}
