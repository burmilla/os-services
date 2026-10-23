// fleet-client reports the state of a BurmillaOS server to Fleet Manager
// and applies the target OS / User Docker versions and scheduled restarts
// it gets back, inside the server's maintenance window and only while it
// holds a maintenance lock.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
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
	SSH                     SSH      `json:"ssh"`
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
	// Service counters are only filled in by managers. Jobs are skipped.
	Services         int            `json:"services"`
	ServicesReady    int            `json:"services_ready"`
	ServicesNotReady []ServiceState `json:"services_not_ready"`
}

// ServiceState describes a Swarm service which does not run all of its
// tasks, or is in the middle of an update, with its current spec.
type ServiceState struct {
	Name         string `json:"name"`
	Mode         string `json:"mode"`
	DesiredTasks int    `json:"desired_tasks"`
	RunningTasks int    `json:"running_tasks"`
	UpdateState  string `json:"update_state"`
}

type Report struct {
	Hostname      string           `json:"hostname"`
	Swarm         string           `json:"swarm"`
	Environment   string           `json:"environment"`
	OSVersion     string           `json:"os_version"`
	DockerVersion string           `json:"docker_version"`
	KernelVersion string           `json:"kernel_version"`
	ClientVersion string           `json:"client_version"`
	BootTime      time.Time        `json:"boot_time"`
	State         string           `json:"state"`
	Message       string           `json:"message"`
	SwarmStatus   *SwarmStatus     `json:"swarm_status,omitempty"`
	Containers    *ContainerStatus `json:"containers,omitempty"`
	SSH           *SSH             `json:"ssh,omitempty"`
}

type client struct {
	manager    string
	api        string // base path, names the Swarm + Environment
	user, pass string
	swarm      string
	env        string
	hostname   string
	interval   time.Duration
	http       *http.Client
	docker     *http.Client
	homeDir    string
	ssh        *SSH // state of the last applied SSH settings
	state, msg string
}

func main() {
	c := &client{
		manager:  strings.TrimRight(os.Getenv("FLEET_MANAGER_URL"), "/"),
		swarm:    os.Getenv("FLEET_SWARM"),
		env:      os.Getenv("FLEET_ENVIRONMENT"),
		hostname: os.Getenv("FLEET_HOSTNAME"),
		user:     os.Getenv("FLEET_USERNAME"),
		pass:     os.Getenv("FLEET_PASSWORD"),
		http:     &http.Client{Timeout: 30 * time.Second, Transport: transport(os.Getenv("FLEET_CA_CERT"))},
		docker:   unixClient(envOr("FLEET_DOCKER_SOCK", "/run/docker.sock")),
		homeDir:  "/home",
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
	api, err := apiBase(c.swarm, c.env)
	if err != nil {
		log.Fatal(err)
	}
	c.api = api
	log.Printf("fleet-client %s: %s (swarm %q, environment %q) reporting to %s", Version, c.hostname, c.swarm, c.env, c.manager)

	for {
		time.Sleep(c.run())
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
	if c.interval > 0 {
		interval = c.interval
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	// SSH settings do not interrupt workloads, so they are applied right
	// away and before a possible reboot.
	ssh, recheck, sshErr := c.applySSH(d.SSH, rep.BootTime)
	if recheck > 0 && recheck < interval {
		interval = recheck
	}
	sshChanged := !reflect.DeepEqual(ssh, c.ssh)
	c.ssh, rep.SSH = ssh, ssh

	state, msg := c.act(d, rep)
	if sshErr != nil && state == "idle" {
		state, msg = "error", "ssh: "+sshErr.Error()
	}
	if state != c.state || msg != c.msg || sshChanged {
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

// capture runs a command and returns its output, with the last line of
// the output in the error.
func capture(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return "", fmt.Errorf("%v: %s", err, msg)
	}
	return strings.TrimSpace(out.String()), nil
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
		SSH:           c.ssh,
	}
	st, err := c.swarmStatus()
	if err != nil {
		log.Printf("cannot read Swarm status: %v", err)
	} else {
		rep.SwarmStatus = st
	}
	cs, err := c.containerStatus()
	if err != nil {
		log.Printf("cannot read container status: %v", err)
	} else {
		rep.Containers = cs
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
	if err := c.serviceStatus(st); err != nil {
		return nil, err
	}
	return st, nil
}

// serviceStatus counts the services which run all their desired tasks
// with their current spec. Swarm reports a task with a health check as
// running only once it is healthy, so running means healthy here.
func (c *client) serviceStatus(st *SwarmStatus) error {
	var services []struct {
		ID   string
		Spec struct {
			Name string
			Mode struct {
				Replicated    *struct{ Replicas *int }
				Global        *struct{}
				ReplicatedJob *struct{}
				GlobalJob     *struct{}
			}
		}
		ServiceStatus *struct {
			RunningTasks int
			DesiredTasks int
		}
		UpdateStatus *struct{ State string }
	}
	if err := c.dockerGet("/services?status=true", &services); err != nil {
		return err
	}
	// Docker before 20.10 (API 1.41) ignores status=true: count the tasks.
	var tasks map[string]taskCount
	for _, svc := range services {
		if svc.ServiceStatus == nil {
			var err error
			if tasks, err = c.taskCounts(); err != nil {
				return err
			}
			break
		}
	}
	st.ServicesNotReady = []ServiceState{}
	for _, svc := range services {
		mode := "replicated"
		switch {
		case svc.Spec.Mode.ReplicatedJob != nil, svc.Spec.Mode.GlobalJob != nil:
			continue
		case svc.Spec.Mode.Global != nil:
			mode = "global"
		}
		s := ServiceState{Name: svc.Spec.Name, Mode: mode}
		if svc.ServiceStatus != nil {
			s.DesiredTasks, s.RunningTasks = svc.ServiceStatus.DesiredTasks, svc.ServiceStatus.RunningTasks
		} else {
			n := tasks[svc.ID]
			s.DesiredTasks, s.RunningTasks = n.desired, n.running
			if r := svc.Spec.Mode.Replicated; r != nil && r.Replicas != nil {
				s.DesiredTasks = *r.Replicas
			}
		}
		if svc.UpdateStatus != nil {
			s.UpdateState = svc.UpdateStatus.State
		}
		st.Services++
		if s.RunningTasks >= s.DesiredTasks && !updateInProgress(s.UpdateState) {
			st.ServicesReady++
		} else {
			st.ServicesNotReady = append(st.ServicesNotReady, s)
		}
	}
	return nil
}

type taskCount struct{ desired, running int }

// taskCounts counts per service the tasks which should run and the ones
// which do, like ServiceStatus of newer Docker versions.
func (c *client) taskCounts() (map[string]taskCount, error) {
	var tasks []struct {
		ServiceID    string
		DesiredState string
		Status       struct{ State string }
	}
	if err := c.dockerGet("/tasks", &tasks); err != nil {
		return nil, err
	}
	out := map[string]taskCount{}
	for _, t := range tasks {
		if t.DesiredState != "running" {
			continue
		}
		n := out[t.ServiceID]
		n.desired++
		if t.Status.State == "running" {
			n.running++
		}
		out[t.ServiceID] = n
	}
	return out, nil
}

// updateInProgress reports whether a service update has not reached its
// target configuration yet. A completed rollback is a finished state: the
// service runs its previous spec again.
func updateInProgress(state string) bool {
	switch state {
	case "updating", "paused", "rollback_started", "rollback_paused":
		return true
	}
	return false
}

// apiBase returns the Fleet Manager API path of a server. The Swarm and
// Environment in the path are what make the server a member of them.
func apiBase(swarm, env string) (string, error) {
	switch {
	case swarm == "" && env == "":
		return "/api/v1", nil
	case swarm == "" || env == "":
		return "", fmt.Errorf("FLEET_SWARM and FLEET_ENVIRONMENT must be set together")
	}
	return "/api/v1/swarms/" + url.PathEscape(swarm) + "/" + url.PathEscape(env), nil
}

func (c *client) post(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.manager+c.api+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	return c.http.Do(req)
}

// transport trusts the extra CA in caFile, e.g. for a Fleet Manager with a
// certificate from a private CA.
func transport(caFile string) http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if caFile == "" {
		return t
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatalf("read FLEET_CA_CERT: %v", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		log.Fatalf("no certificates in FLEET_CA_CERT %s", caFile)
	}
	t.TLSClientConfig = &tls.Config{RootCAs: pool}
	return t
}

func (c *client) report(rep *Report) (*Desired, error) {
	resp, err := c.post("/report", rep)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fleet manager returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	d := &Desired{}
	return d, json.NewDecoder(resp.Body).Decode(d)
}

func (c *client) lock(reason string) (bool, string, error) {
	resp, err := c.post("/maintenance/lock", map[string]string{"hostname": c.hostname, "reason": reason})
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
