package internal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// How often the daemon checks that the device is still there, and reconnects if it isn't
const probeInterval = 15 * time.Second

// A refresh asks every guest for its IP, which can take a while on a big cluster
const clientTimeout = 2 * time.Minute

// How long StartDaemon and StopDaemon wait for the socket to come up or go away
const startStopTimeout = 10 * time.Second

// ErrNoDaemon is returned by CallDaemon when no daemon is listening
var ErrNoDaemon = errors.New("daemon not running")

// A CLI request: one command line in (refresh, next, reload, status, stop), one reply line out,
// like the firmware's protocol: "OK <message>" or "ERR <message>"
type pending struct {
	cmd   string
	reply chan string
	sent  chan struct{} // closed once the reply is written
}

// server is the daemon's state, used only from its main goroutine
type server struct {
	drv     *Driver
	lastErr error
	// onReload resets the timers to the reloaded config
	onReload func(Config)
}

// socketPath is where the daemon listens for CLI requests: next to the config file, or in the
// temp directory when that path is too long for a socket
func socketPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "proxstat.sock")
	// Socket paths are limited to about 100 bytes; a deep config directory falls back to temp
	if len(path) > 100 {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("proxstat-%d.sock", os.Getuid()))
	}
	return path, nil
}

// RunDaemon polls Proxmox on the update interval, sends redraws and liveness probes, and serves CLI
// requests until it gets SIGINT or SIGTERM. Everything touching the device runs on this goroutine.
func RunDaemon() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	sock, err := socketPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return err
	}
	if conn, err := net.Dial("unix", sock); err == nil {
		conn.Close()
		return errors.New("daemon is already running")
	}
	os.Remove(sock) // left behind by a daemon that was killed
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sock, err)
	}
	defer ln.Close()
	if err := os.Chmod(sock, 0o600); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	requests := make(chan pending)
	go serveRequests(ctx, ln, requests)

	updateTicker := time.NewTicker(time.Hour)
	redrawTicker := time.NewTicker(time.Hour)
	probeTicker := time.NewTicker(probeInterval)
	defer updateTicker.Stop()
	defer redrawTicker.Stop()
	defer probeTicker.Stop()
	updates := resetTicker(updateTicker, cfg.updateInterval())
	redraws := resetTicker(redrawTicker, cfg.redrawInterval())

	s := &server{
		drv: NewDriver(cfg, true),
		onReload: func(newCfg Config) {
			updates = resetTicker(updateTicker, newCfg.updateInterval())
			redraws = resetTicker(redrawTicker, newCfg.redrawInterval())
		},
	}
	defer s.drv.Close()
	log.Printf("Polling every %d min, device VID %s PID %s, socket %s", cfg.UpdateIntervalMinutes, cfg.VID, cfg.PID, sock)
	warnings, err := s.drv.Refresh()
	s.note("update", warnings, err)

	for {
		select {
		case <-ctx.Done():
			log.Print("Stopping")
			return nil
		case <-updates:
			warnings, err := s.drv.Refresh()
			s.note("update", warnings, err)
		case <-redraws:
			warnings, err := s.drv.Push("full")
			s.note("redraw", warnings, err)
		case <-probeTicker.C:
			// "ver" changes nothing on screen; it only finds out whether the device is still there
			warnings, err := s.drv.Push("ver")
			s.note("probe", warnings, err)
		case p := <-requests:
			if p.cmd == "stop" {
				p.reply <- "OK stopped"
				<-p.sent // the CLI gets its answer before the socket goes away
				log.Print("Stopping")
				return nil
			}
			msg, err := s.handle(p.cmd)
			if err != nil {
				p.reply <- "ERR " + err.Error()
			} else {
				p.reply <- "OK " + msg
			}
		}
	}
}

// handle runs one CLI request on the daemon's goroutine, returning the message to show
func (s *server) handle(cmd string) (string, error) {
	var warnings []string
	var err error
	msg := ""
	switch cmd {
	case "refresh":
		warnings, err = s.drv.Refresh()
		msg = "refreshed, " + changesText(s.drv.lastChanges)
	case "next":
		warnings, err = s.drv.Push("next")
		msg = "next page"
	case "reload":
		var cfg Config
		if cfg, err = LoadConfig(); err != nil {
			break
		}
		s.drv.setConfig(cfg)
		s.onReload(cfg)
		warnings, err = s.drv.Push()
		msg = "applied"
		if errors.Is(err, ErrNoDevice) {
			// saved, and sent on the next connect
			return "saved; display not connected, it gets the setting on the next connect", nil
		}
	case "status":
		return s.status(), nil
	default:
		return "", fmt.Errorf("unknown request %q", cmd)
	}
	s.note(cmd, warnings, err)
	if err != nil {
		return "", err
	}
	if len(warnings) > 0 {
		return "", errors.New("device rejected " + strings.Join(warnings, "; "))
	}
	return msg, nil
}

// note logs an error only when it differs from the last one, so a missing device doesn't log
// every probe
func (s *server) note(what string, warnings []string, err error) {
	for _, w := range warnings {
		log.Printf("%s: device rejected %s", what, w)
	}
	if err != nil && (s.lastErr == nil || s.lastErr.Error() != err.Error()) {
		log.Printf("%s: %v", what, err)
	}
	s.lastErr = err
}

// status describes the connection, the last successful update and the last error
func (s *server) status() string {
	var b strings.Builder
	if port := s.drv.Port(); port != "" {
		fmt.Fprintf(&b, "connected to %s", port)
	} else {
		b.WriteString("device not connected")
	}
	if !s.drv.lastUpdate.IsZero() {
		fmt.Fprintf(&b, ", last update %s (%s)", s.drv.lastUpdate.Format(time.DateTime), changesText(s.drv.lastChanges))
	}
	if s.lastErr != nil {
		fmt.Fprintf(&b, ", last error: %v", s.lastErr)
	}
	return b.String()
}

// changesText describes how many lines an update sent
func changesText(n int) string {
	switch n {
	case 0:
		return "nothing changed"
	case 1:
		return "1 line sent"
	}
	return fmt.Sprintf("%d lines sent", n)
}

// resetTicker restarts t at the new interval and returns its channel, or nil (never fires) when off
func resetTicker(t *time.Ticker, every time.Duration) <-chan time.Time {
	if every <= 0 {
		t.Stop()
		return nil
	}
	t.Reset(every)
	return t.C
}

// serveRequests accepts CLI connections and hands each request to the daemon loop, writing its
// response back, until the listener closes
func serveRequests(ctx context.Context, ln net.Listener, requests chan<- pending) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed on shutdown
		}
		// One request and one response per connection
		go func() {
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(clientTimeout))
			cmd, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				return
			}
			p := pending{cmd: strings.TrimSpace(cmd), reply: make(chan string, 1), sent: make(chan struct{})}
			defer close(p.sent)
			select {
			case requests <- p:
			case <-ctx.Done():
				return
			}
			select {
			case reply := <-p.reply:
				fmt.Fprintln(conn, strings.ReplaceAll(reply, "\n", " "))
			case <-ctx.Done():
			}
		}()
	}
}

// LogPath is the daemon's log file in the per-user cache directory, since a background daemon
// has no terminal to log to
func LogPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find cache directory: %w", err)
	}
	return filepath.Join(base, "proxstat", "daemon.log"), nil
}

// daemonListening reports whether a daemon accepts connections on the socket
func daemonListening() bool {
	sock, err := socketPath()
	if err != nil {
		return false
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// StartDaemon runs "daemon --foreground" as a detached background process logging to the log
// file, and waits until it listens on the socket. It returns the process id and the log file.
func StartDaemon() (int, string, error) {
	if daemonListening() {
		return 0, "", errors.New("daemon is already running")
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	logFile, err := LogPath()
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o700); err != nil {
		return 0, "", err
	}
	out, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, "", fmt.Errorf("open log: %w", err)
	}
	defer out.Close() // the daemon has its own copy

	cmd := exec.Command(exe, "daemon", "--foreground")
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("start daemon: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	timeout := time.After(startStopTimeout)
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-exited:
			return 0, logFile, fmt.Errorf("daemon exited: %s", lastLogLine(logFile))
		case <-timeout:
			return cmd.Process.Pid, logFile, fmt.Errorf("daemon didn't start listening within %s, see %s", startStopTimeout, logFile)
		case <-poll.C:
			if daemonListening() {
				return cmd.Process.Pid, logFile, nil
			}
		}
	}
}

// lastLogLine returns the log's last line, which says why a daemon that just started exited
func lastLogLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "see " + path
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return strings.TrimPrefix(lines[len(lines)-1], "proxstat: ")
}

// StopDaemon asks the daemon to exit and waits until its socket is gone
func StopDaemon() error {
	if _, err := CallDaemon("stop"); err != nil {
		return err
	}
	for deadline := time.Now().Add(startStopTimeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if !daemonListening() {
			return nil
		}
	}
	return fmt.Errorf("daemon didn't stop within %s", startStopTimeout)
}

// CallDaemon sends one command to the daemon and returns its message. A command the daemon
// couldn't carry out comes back as an error, and ErrNoDaemon means nothing is listening.
func CallDaemon(cmd string) (string, error) {
	sock, err := socketPath()
	if err != nil {
		return "", err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return "", ErrNoDaemon
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(clientTimeout))
	if _, err := fmt.Fprintln(conn, cmd); err != nil {
		return "", err
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read daemon reply: %w", err)
	}
	reply = strings.TrimSuffix(reply, "\n")
	if msg, ok := strings.CutPrefix(reply, "OK "); ok {
		return msg, nil
	}
	if msg, ok := strings.CutPrefix(reply, "ERR "); ok {
		return "", errors.New(msg)
	}
	return "", fmt.Errorf("bad daemon reply %q", reply)
}
