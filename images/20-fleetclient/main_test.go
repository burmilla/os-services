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
		}
	})}}
	srv.Start()
	defer srv.Close()

	c := &client{docker: unixClient(sock)}
	st, err := c.swarmStatus()
	if err != nil {
		t.Fatal(err)
	}
	want := SwarmStatus{NodeID: "n1", ClusterID: "c1", LocalNodeState: "active", Manager: true, Nodes: 3, NodesReady: 2, Managers: 2, ManagersReachable: 1}
	if *st != want {
		t.Fatalf("got %+v", *st)
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
