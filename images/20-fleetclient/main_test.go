package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSwarmStatus(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.Write([]byte(`{"Swarm":{"NodeID":"n1","LocalNodeState":"active","ControlAvailable":true,"Cluster":{"ID":"c1"}}}`))
		case "/nodes":
			w.Write([]byte(`[
				{"Status":{"State":"ready"},"ManagerStatus":{"Reachability":"reachable"}},
				{"Status":{"State":"down"},"ManagerStatus":{"Reachability":"unreachable"}},
				{"Status":{"State":"ready"}}]`))
		case "/services":
			if r.URL.Query().Get("status") != "true" {
				t.Errorf("service status not requested")
			}
			w.Write([]byte(`[
				{"Spec":{"Name":"web","Mode":{"Replicated":{"Replicas":3}}},"ServiceStatus":{"RunningTasks":3,"DesiredTasks":3}},
				{"Spec":{"Name":"api","Mode":{"Replicated":{"Replicas":2}}},"ServiceStatus":{"RunningTasks":2,"DesiredTasks":2},"UpdateStatus":{"State":"paused"}},
				{"Spec":{"Name":"agent","Mode":{"Global":{}}},"ServiceStatus":{"RunningTasks":2,"DesiredTasks":3}},
				{"Spec":{"Name":"old","Mode":{"Replicated":{}}},"ServiceStatus":{"RunningTasks":1,"DesiredTasks":1},"UpdateStatus":{"State":"rollback_completed"}},
				{"Spec":{"Name":"migrate","Mode":{"ReplicatedJob":{}}},"ServiceStatus":{"RunningTasks":0,"DesiredTasks":1}}]`))
		}
	})}}
	srv.Start()
	defer srv.Close()

	c := &client{docker: unixClient(sock)}
	st, err := c.swarmStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.NodeID != "n1" || st.ClusterID != "c1" || st.LocalNodeState != "active" || !st.Manager ||
		st.Nodes != 3 || st.NodesReady != 2 || st.Managers != 2 || st.ManagersReachable != 1 {
		t.Fatalf("got %+v", *st)
	}
	if st.Services != 4 || st.ServicesReady != 2 || len(st.ServicesNotReady) != 2 {
		t.Fatalf("unexpected services: %+v", *st)
	}
	if got := st.ServicesNotReady[0]; got != (ServiceState{Name: "api", Mode: "replicated", DesiredTasks: 2, RunningTasks: 2, UpdateState: "paused"}) {
		t.Fatalf("api: %+v", got)
	}
	if got := st.ServicesNotReady[1]; got != (ServiceState{Name: "agent", Mode: "global", DesiredTasks: 3, RunningTasks: 2}) {
		t.Fatalf("agent: %+v", got)
	}
}

func TestCredentialsSent(t *testing.T) {
	mgr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "s1-prod" || p != "secret" {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"swarm":"s1"}`))
	}))
	defer mgr.Close()
	c := &client{manager: mgr.URL, hostname: "n1", http: mgr.Client()}
	if _, err := c.report(&Report{}); err == nil || err.Error() != "fleet manager returned 401 Unauthorized: invalid credentials" {
		t.Fatalf("expected 401, got %v", err)
	}
	c.user, c.pass = "s1-prod", "secret"
	if d, err := c.report(&Report{}); err != nil || d.Swarm != "s1" {
		t.Fatalf("report with credentials: %v %+v", err, d)
	}
}

func TestActWaits(t *testing.T) {
	var lockReason string
	mgr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		lockReason = body["reason"]
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"granted":false,"message":"1 nodes of Swarm s1 are not ready"}`))
	}))
	defer mgr.Close()
	c := &client{manager: mgr.URL, hostname: "n1", http: mgr.Client()}

	d := &Desired{MaintenanceRequired: true, MaintenanceReasons: []string{"restart"}, UpgradePolicy: "auto"}
	if state, msg := c.act(d, &Report{}); state != "waiting" || msg != "pending restart, next maintenance window " {
		t.Fatalf("outside window: %s %s", state, msg)
	}
	d.MaintenanceWindowActive = true
	state, msg := c.act(d, &Report{})
	if state != "waiting" || lockReason != "restart" || msg != "pending restart, maintenance lock refused: 1 nodes of Swarm s1 are not ready" {
		t.Fatalf("lock refused: %s %s", state, msg)
	}
	d.UpgradePolicy = "none"
	if state, _ := c.act(d, &Report{}); state != "waiting" {
		t.Fatal("policy none must not act")
	}
	if state, _ := c.act(&Desired{}, &Report{}); state != "idle" {
		t.Fatal("nothing to do should be idle")
	}
}
