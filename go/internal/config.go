package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config is everything the device would keep in flash if it had any. The device forgets rotation
// and items per page on every reset, so the driver re-sends them with every update.
type Config struct {
	Rotation              int     `json:"rotation"`                // 90 degree steps clockwise, -1..3
	PerPage               int     `json:"per_page"`                // list items per page, 1..4
	UpdateIntervalMinutes int     `json:"update_interval_minutes"` // how often Proxmox is polled
	RedrawIntervalHours   float64 `json:"redraw_interval_hours"`   // full refresh against ghosting, 0 = off
	VID                   string  `json:"vid"`                     // USB vendor id, 4 hex digits
	PID                   string  `json:"pid"`                     // USB product id, 4 hex digits

	HeaderTextColor     FieldColors `json:"header_text_color"`
	ListTextColor       FieldColors `json:"list_text_color"` // offline items keep the default look
	ListBackgroundColor FieldColors `json:"list_background_color"`
}

// FieldColors is one panel color per text field; disk and ram color their icons too
type FieldColors struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Disk string `json:"disk"`
	RAM  string `json:"ram"`
}

// The colors the panel can show
var panelColors = []string{"black", "white", "yellow", "red"}

// field returns the color of the CLI field name, nil for an unknown field
func (f *FieldColors) field(name string) *string {
	switch name {
	case "name":
		return &f.Name
	case "ip":
		return &f.IP
	case "disk":
		return &f.Disk
	case "ram":
		return &f.RAM
	}
	return nil
}

// values returns the colors in firmware field order: name, ip, disk, ram
func (f FieldColors) values() [4]string {
	return [4]string{f.Name, f.IP, f.Disk, f.RAM}
}

// String lists the colors as "name=yellow ip=white disk=white ram=white"
func (f FieldColors) String() string {
	return fmt.Sprintf("name=%s ip=%s disk=%s ram=%s", f.Name, f.IP, f.Disk, f.RAM)
}

// defaultConfig returns the settings used when the config file is missing or leaves a key out
func defaultConfig() Config {
	return Config{
		Rotation:              0,
		PerPage:               4,
		UpdateIntervalMinutes: 10,
		RedrawIntervalHours:   12,
		VID:                   "B00B",
		PID:                   "6969",
		HeaderTextColor:       FieldColors{Name: "yellow", IP: "white", Disk: "white", RAM: "white"},
		ListTextColor:         FieldColors{Name: "black", IP: "black", Disk: "black", RAM: "black"},
		ListBackgroundColor:   FieldColors{Name: "white", IP: "white", Disk: "white", RAM: "white"},
	}
}

var hexID = regexp.MustCompile(`^[0-9A-F]{4}$`)

// validate checks every setting against the firmware's and the driver's limits
func (c Config) validate() error {
	if c.Rotation < -1 || c.Rotation > 3 {
		return errors.New("rotate must be -1..3")
	}
	if c.PerPage < 1 || c.PerPage > 4 {
		return errors.New("per-page must be 1..4")
	}
	if c.UpdateIntervalMinutes < 1 {
		return errors.New("update-interval must be at least 1 minute")
	}
	if c.RedrawIntervalHours < 0 {
		return errors.New("redraw-interval must be 0 (off) or more hours")
	}
	if !hexID.MatchString(c.VID) {
		return errors.New("vid must be 4 hex digits")
	}
	if !hexID.MatchString(c.PID) {
		return errors.New("pid must be 4 hex digits")
	}
	for _, set := range []struct {
		name   string
		colors FieldColors
	}{
		{"header text-color", c.HeaderTextColor},
		{"list text-color", c.ListTextColor},
		{"list background-color", c.ListBackgroundColor},
	} {
		for i, color := range set.colors.values() {
			if !slices.Contains(panelColors, color) {
				field := [4]string{"name", "ip", "disk", "ram"}[i]
				return fmt.Errorf("%s %s: %q is not one of %s", set.name, field, color, strings.Join(panelColors, ", "))
			}
		}
	}
	return nil
}

// updateInterval is the time between Proxmox polls
func (c Config) updateInterval() time.Duration {
	return time.Duration(c.UpdateIntervalMinutes) * time.Minute
}

// redrawInterval is the time between full refreshes, 0 when they are off
func (c Config) redrawInterval() time.Duration {
	return time.Duration(c.RedrawIntervalHours * float64(time.Hour))
}

// SetColor stores one field's color: scope header or list, kind text-color or background-color
// (list only), field name, ip, disk or ram
func (c *Config) SetColor(scope, kind, field, color string) error {
	var colors *FieldColors
	switch {
	case scope == "header" && kind == "text-color":
		colors = &c.HeaderTextColor
	case scope == "list" && kind == "text-color":
		colors = &c.ListTextColor
	case scope == "list" && kind == "background-color":
		colors = &c.ListBackgroundColor
	case scope == "header" && kind == "background-color":
		return errors.New("the header has no background-color, only text-color")
	case scope != "header" && scope != "list":
		return fmt.Errorf("unknown setting %q, expected header or list", scope)
	default:
		return fmt.Errorf("unknown %s setting %q, expected text-color or background-color", scope, kind)
	}
	target := colors.field(field)
	if target == nil {
		return fmt.Errorf("unknown field %q, expected name, ip, disk or ram", field)
	}
	*target = strings.ToLower(color)
	return c.validate()
}

// Set parses value for the CLI key and stores it
func (c *Config) Set(key, value string) error {
	var err error
	switch key {
	case "rotate":
		c.Rotation, err = strconv.Atoi(value)
	case "per-page":
		c.PerPage, err = strconv.Atoi(value)
	case "update-interval":
		c.UpdateIntervalMinutes, err = strconv.Atoi(value)
	case "redraw-interval":
		c.RedrawIntervalHours, err = strconv.ParseFloat(value, 64)
	case "vid":
		c.VID = strings.ToUpper(strings.TrimPrefix(strings.ToLower(value), "0x"))
	case "pid":
		c.PID = strings.ToUpper(strings.TrimPrefix(strings.ToLower(value), "0x"))
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	if err != nil {
		return fmt.Errorf("%s: %q is not a number", key, value)
	}
	return c.validate()
}

// configDir is the per-user config location for this OS: $XDG_CONFIG_HOME or ~/.config on Linux,
// ~/Library/Application Support on macOS, %AppData% on Windows.
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(base, "proxstat"), nil
}

// ConfigPath is the location of config.json inside configDir
func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig reads the config file, falling back to defaults for a missing file or missing keys
func LoadConfig() (Config, error) {
	cfg := defaultConfig()
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig writes the config through a temp file so a crash never leaves it half written
func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
