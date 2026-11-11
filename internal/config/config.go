// Package config reads fixpoint.json and fills in defaults, so every other
// package can rely on complete settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Config is the whole of fixpoint.json.
type Config struct {
	Target   Target   `json:"target"`
	Kane     Kane     `json:"kane"`
	Agent    Agent    `json:"agent"`
	Pipeline Pipeline `json:"pipeline"`
	Server   Server   `json:"server"`
}

// Target is the app being watched and fixed. An empty URL means demo mode.
type Target struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Dir        string `json:"dir"`
	Notes      string `json:"notes"`
	RestartCmd string `json:"restart_cmd"`
	HealthURL  string `json:"health_url"`
}

// Kane holds the Kane CLI settings.
type Kane struct {
	Bin          string `json:"bin"`
	TimeoutS     int    `json:"timeout_s"`
	MaxSteps     int    `json:"max_steps"`
	BugDetection string `json:"bug_detection"`
	Headless     *bool  `json:"headless"`
}

// Agent holds the coding agent settings.
type Agent struct {
	Preset   string `json:"preset"`
	Bin      string `json:"bin"`
	Model    string `json:"model"`
	TimeoutS int    `json:"timeout_s"`
}

// Pipeline holds the loop settings.
type Pipeline struct {
	MaxAttempts   int    `json:"max_attempts"`
	Autonomy      string `json:"autonomy"`
	MaxInfoRounds int    `json:"max_info_rounds"`
}

// Server holds the listen addresses.
type Server struct {
	Addr     string `json:"addr"`
	ShopAddr string `json:"shop_addr"`
}

// Default returns the settings used when fixpoint.json is absent.
func Default() Config {
	t := true
	return Config{
		Kane:     Kane{Bin: "kane-cli", TimeoutS: 240, MaxSteps: 30, BugDetection: "continue", Headless: &t},
		Agent:    Agent{Preset: "claude", Bin: "claude", TimeoutS: 600},
		Pipeline: Pipeline{MaxAttempts: 3, Autonomy: "auto", MaxInfoRounds: 2},
		Server:   Server{Addr: "127.0.0.1:7777", ShopAddr: "127.0.0.1:4000"},
	}
}

// Load reads path (a missing file is not an error) and fills defaults.
func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s is not valid JSON (%v); fix it or delete it to use the defaults", path, err)
	}
	c.fill()
	return c, c.Validate()
}

func (c *Config) fill() {
	d := Default()
	if c.Kane.Bin == "" {
		c.Kane.Bin = d.Kane.Bin
	}
	if c.Kane.TimeoutS <= 0 {
		c.Kane.TimeoutS = d.Kane.TimeoutS
	}
	if c.Kane.MaxSteps <= 0 {
		c.Kane.MaxSteps = d.Kane.MaxSteps
	}
	if c.Kane.Headless == nil {
		c.Kane.Headless = d.Kane.Headless
	}
	if c.Agent.Preset == "" {
		c.Agent.Preset = d.Agent.Preset
	}
	if c.Agent.Bin == "" {
		c.Agent.Bin = d.Agent.Bin
	}
	if c.Agent.TimeoutS <= 0 {
		c.Agent.TimeoutS = d.Agent.TimeoutS
	}
	if c.Pipeline.MaxAttempts <= 0 {
		c.Pipeline.MaxAttempts = d.Pipeline.MaxAttempts
	}
	if c.Pipeline.Autonomy == "" {
		c.Pipeline.Autonomy = d.Pipeline.Autonomy
	}
	if c.Pipeline.MaxInfoRounds <= 0 {
		c.Pipeline.MaxInfoRounds = d.Pipeline.MaxInfoRounds
	}
	if c.Server.Addr == "" {
		c.Server.Addr = d.Server.Addr
	}
	if c.Server.ShopAddr == "" {
		c.Server.ShopAddr = d.Server.ShopAddr
	}
}

// Validate reports settings that cannot work.
func (c Config) Validate() error {
	if c.Pipeline.Autonomy != "auto" && c.Pipeline.Autonomy != "ask" {
		return fmt.Errorf(`pipeline.autonomy is %q; use "auto" or "ask"`, c.Pipeline.Autonomy)
	}
	if c.Agent.Preset != "claude" {
		return fmt.Errorf(`agent.preset is %q; the only preset is "claude"`, c.Agent.Preset)
	}
	if c.Target.URL != "" && c.Target.Dir == "" {
		return errors.New("target.url is set but target.dir is empty; set target.dir to the app's source directory")
	}
	return nil
}

// Demo reports whether Fixpoint runs against the built-in demo shop.
func (c Config) Demo() bool { return c.Target.URL == "" }

// Headless reports the effective headless setting.
func (c Config) Headless() bool { return c.Kane.Headless == nil || *c.Kane.Headless }

// OriginFor turns a listen address into the URL a browser on this machine
// uses, e.g. "127.0.0.1:7777" -> "http://localhost:7777".
func OriginFor(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || isLoopback(host) {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Starter is the commented fixpoint.json that `fixpoint init` writes. JSON
// has no comments, so the notes live in "//" keys, which Load ignores.
const Starter = `{
  "//": "Fixpoint settings. Every key is optional. Delete this file to run the built-in demo.",
  "target": {
    "//": "Your app: the URL Kane opens, the source directory the agent patches, and how to restart it after a patch.",
    "name": "My app",
    "url": "http://localhost:3000",
    "dir": ".",
    "notes": "Anything the agent should know: test data, where prices come from, pages that need a login.",
    "restart_cmd": "",
    "health_url": ""
  },
  "kane": {
    "//": "Kane CLI runs every check in a real browser. Sign in once with: kane-cli login",
    "bin": "kane-cli",
    "timeout_s": 240,
    "max_steps": 30,
    "bug_detection": "continue",
    "headless": true
  },
  "agent": {
    "//": "The coding agent that writes checks and patches. Only the Claude Code preset exists.",
    "preset": "claude",
    "bin": "claude",
    "model": "",
    "timeout_s": 600
  },
  "pipeline": {
    "//": "autonomy: auto fixes straight away; ask waits for you to approve each fix.",
    "max_attempts": 3,
    "autonomy": "auto",
    "max_info_rounds": 2
  },
  "server": {
    "addr": "127.0.0.1:7777",
    "shop_addr": "127.0.0.1:4000"
  }
}
`
