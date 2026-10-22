// fleet-client reports the state of a BurmillaOS server to Fleet Manager
// and applies the target OS / User Docker versions and scheduled restarts
// it gets back, inside the server's maintenance window and only while it
// holds a maintenance lock.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var Version = "dev"

const (
	rosBin       = "/usr/bin/ros"
	systemDocker = "/usr/bin/system-docker"
)

// Desired is the response of the Fleet Manager config and report APIs.
type Desired struct {
	Swarm                   string   `json:"swarm"`
	Environment             string   `json:"environment"`
	OSImage                 string   `json:"os_image"`
	OSVersion               string   `json:"os_version"`
	DockerVersion           string   `json:"docker_version"`
	UpgradePolicy           string   `json:"upgrade_policy"`
	CheckIntervalSeconds    int      `json:"check_interval_seconds"`
	MaintenanceWindowActive bool     `json:"maintenance_window_active"`
	MaintenanceWindowNext   string   `json:"maintenance_window_next_start"`
	MaintenanceRequired     bool     `json:"maintenance_required"`
	MaintenanceReasons      []string `json:"maintenance_reasons"`
}

type SwarmStatus struct {
	NodeID            string `json:"node_id"`
	ClusterID         string `json:"cluster_id"`
	LocalNodeState    string `json:"local_node_state"`
	Manager           bool   `json:"manager"`
	Nodes             int    `json:"nodes"`
	NodesReady        int    `json:"nodes_ready"`
	Managers          int    `json:"managers"`
	ManagersReachable int    `json:"managers_reachable"`
}

type Report struct {
	Hostname      string       `json:"hostname"`
	Swarm         string       `json:"swarm"`
	Environment   string       `json:"environment"`
	OSVersion     string       `json:"os_version"`
	DockerVersion string       `json:"docker_version"`
	KernelVersion string       `json:"kernel_version"`
	ClientVersion string       `json:"client_version"`
	BootTime      time.Time    `json:"boot_time"`
	State         string       `json:"state"`
	Message       string       `json:"message"`
	SwarmStatus   *SwarmStatus `json:"swarm_status,omitempty"`
}

type client struct {
	manager    string
	swarm      string
	env        string
	hostname   string
	interval   time.Duration
	http       *http.Client
	docker     *http.Client
	state, msg string
}

func main() {
	c := &client{
		manager:  strings.TrimRight(os.Getenv("FLEET_MANAGER_URL"), "/"),
		swarm:    os.Getenv("FLEET_SWARM"),
		env:      os.Getenv("FLEET_ENVIRONMENT"),
		hostname: os.Getenv("FLEET_HOSTNAME"),
		http:     &http.Client{Timeout: 30 * time.Second},
		docker:   unixClient(envOr("FLEET_DOCKER_SOCK", "/run/docker.sock")),
		state:    "idle",
	}
	if c.manager == "" {
		log.Fatal("FLEET_MANAGER_URL is not set")
	}
	if v := os.Getenv("FLEET_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid FLEET_INTERVAL: %v", err)
		}
		c.interval = d
	}
	if c.hostname == "" {
		c.hostname, _ = os.Hostname()
	}
	log.Printf("fleet-client %s: %s (swarm %q, environment %q) reporting to %s", Version, c.hostname, c.swarm, c.env, c.manager)

	for {
		next := c.run()
		if c.interval > 0 {
			next = c.interval
		}
		time.Sleep(next)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func unixClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}
}

// run does one report + act cycle and returns when to run the next one.
func (c *client) run() time.Duration {
	rep := c.collect()
	d, err := c.report(rep)
	if err != nil {
		log.Printf("report failed: %v", err)
		return time.Minute
	}
	interval := time.Duration(d.CheckIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	state, msg := c.act(d, rep)
	if state != c.state || msg != c.msg {
		if msg != "" {
			log.Printf("%s: %s", state, msg)
		}
		c.state, c.msg = state, msg
		// Let the manager know right away, e.g. about errors.
		rep.State, rep.Message = state, msg
		if _, err := c.report(rep); err != nil {
			log.Printf("report failed: %v", err)
		}
	}
	return interval
}

func (c *client) act(d *Desired, rep *Report) (string, string) {
	if !d.MaintenanceRequired {
		return "idle", ""
	}
	pending := strings.Join(d.MaintenanceReasons, ", ")
	switch d.UpgradePolicy {
	case "download":
		if contains(d.MaintenanceReasons, "os_upgrade") {
			image := d.OSImage + ":" + d.OSVersion
			if err := run(systemDocker, "pull", image); err != nil {
				return "error", fmt.Sprintf("pull %s: %v", image, err)
			}
		}
		return "waiting", "pending " + pending + " (upgrade_policy download, images pulled)"
	case "auto":
	default:
		return "waiting", "pending " + pending + " (upgrade_policy " + d.UpgradePolicy + ")"
	}

	if !d.MaintenanceWindowActive {
		return "waiting", "pending " + pending + ", next maintenance window " + d.MaintenanceWindowNext
	}
	granted, why, err := c.lock(d.MaintenanceReasons[0])
	if err != nil {
		return c.state, ""
	}
	if !granted {
		return "waiting", "pending " + pending + ", maintenance lock refused: " + why
	}

	rep.State, rep.Message = "upgrading", "starting "+pending
	if _, err := c.report(rep); err != nil {
		log.Printf("report failed: %v", err)
	}
	log.Printf("maintenance lock granted, starting %s", pending)

	if contains(d.MaintenanceReasons, "docker_upgrade") {
		if err := run(rosBin, "engine", "switch", "-f", d.DockerVersion); err != nil {
			return "error", fmt.Sprintf("switch User Docker to %s: %v", d.DockerVersion, err)
		}
	}
	if contains(d.MaintenanceReasons, "os_upgrade") {
		image := d.OSImage + ":" + d.OSVersion
		// Reboots the server when done.
		if err := run(rosBin, "os", "upgrade", "-f", "-i", image); err != nil {
			return "error", fmt.Sprintf("upgrade to %s: %v", image, err)
		}
		return "upgrading", "rebooting to " + image
	}
	if contains(d.MaintenanceReasons, "restart") {
		log.Print("scheduled restart")
		// ros acts as reboot when called with that name.
		cmd := &exec.Cmd{Path: rosBin, Args: []string{"reboot"}, Stdout: os.Stdout, Stderr: os.Stderr}
		if err := cmd.Run(); err != nil {
			return "error", fmt.Sprintf("reboot: %v", err)
		}
		return "upgrading", "rebooting"
	}
	// User Docker switched without reboot; the next report releases the
	// lock once the Swarm is healthy again.
	return "idle", ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	os.Stdout.Write(out.Bytes())
	if err != nil {
		msg := strings.TrimSpace(out.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}

func output(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		log.Printf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func (c *client) collect() *Report {
	rep := &Report{
		Hostname:      c.hostname,
		Swarm:         c.swarm,
		Environment:   c.env,
		ClientVersion: Version,
		OSVersion:     output(rosBin, "os", "version"),
		DockerVersion: currentEngine(),
		KernelVersion: kernelVersion(),
		BootTime:      bootTime(),
		State:         c.state,
		Message:       c.msg,
	}
	st, err := c.swarmStatus()
	if err != nil {
		log.Printf("cannot read Swarm status: %v", err)
	} else {
		rep.SwarmStatus = st
	}
	return rep
}

// currentEngine parses the "current  docker-x.y.z" line of ros engine list.
func currentEngine() string {
	for _, line := range strings.Split(output(rosBin, "engine", "list"), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "current" {
			return f[1]
		}
	}
	return ""
}

func kernelVersion() string {
	data, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.TrimSpace(string(data))
}

func bootTime() time.Time {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "btime "); ok {
			sec, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(sec, 0).UTC()
		}
	}
	return time.Time{}
}

func (c *client) dockerGet(path string, v any) error {
	resp, err := c.docker.Get("http://docker" + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *client) swarmStatus() (*SwarmStatus, error) {
	var info struct {
		Swarm struct {
			NodeID           string
			LocalNodeState   string
			ControlAvailable bool
			Cluster          *struct{ ID string }
		}
	}
	if err := c.dockerGet("/info", &info); err != nil {
		return nil, err
	}
	st := &SwarmStatus{
		NodeID:         info.Swarm.NodeID,
		LocalNodeState: info.Swarm.LocalNodeState,
		Manager:        info.Swarm.ControlAvailable,
	}
	if info.Swarm.Cluster != nil {
		st.ClusterID = info.Swarm.Cluster.ID
	}
	if !st.Manager {
		return st, nil
	}
	var nodes []struct {
		Status        struct{ State string }
		ManagerStatus *struct{ Reachability string }
	}
	if err := c.dockerGet("/nodes", &nodes); err != nil {
		return nil, err
	}
	for _, n := range nodes {
		st.Nodes++
		if n.Status.State == "ready" {
			st.NodesReady++
		}
		if n.ManagerStatus != nil {
			st.Managers++
			if n.ManagerStatus.Reachability == "reachable" {
				st.ManagersReachable++
			}
		}
	}
	return st, nil
}

func (c *client) post(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.http.Post(c.manager+path, "application/json", bytes.NewReader(data))
}

func (c *client) report(rep *Report) (*Desired, error) {
	resp, err := c.post("/api/v1/report", rep)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet manager returned %s", resp.Status)
	}
	d := &Desired{}
	return d, json.NewDecoder(resp.Body).Decode(d)
}

func (c *client) lock(reason string) (bool, string, error) {
	resp, err := c.post("/api/v1/maintenance/lock", map[string]string{
		"hostname": c.hostname, "swarm": c.swarm, "environment": c.env, "reason": reason,
	})
	if err != nil {
		log.Printf("maintenance lock request failed: %v", err)
		return false, "", err
	}
	defer resp.Body.Close()
	var r struct {
		Granted bool
		Message string
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode == http.StatusOK && r.Granted, r.Message, nil
}
