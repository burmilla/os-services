package main

import (
	"net/url"
	"strings"
)

// ignoreLabel excludes a container from the health check.
const ignoreLabel = "io.burmilla.fleet.ignore"

// ContainerStatus covers the local User Docker containers which Docker
// restarts on its own (restart policy always, unless-stopped or
// on-failure). Swarm tasks are covered by the Swarm status instead.
type ContainerStatus struct {
	Containers         int              `json:"containers"`
	ContainersReady    int              `json:"containers_ready"`
	ContainersNotReady []ContainerState `json:"containers_not_ready"`
}

type ContainerState struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	Health        string `json:"health"`
	RestartPolicy string `json:"restart_policy"`
	ExitCode      int    `json:"exit_code"`
}

func (c *client) containerStatus() (*ContainerStatus, error) {
	var list []struct {
		ID     string
		Labels map[string]string
	}
	if err := c.dockerGet("/containers/json?all=true", &list); err != nil {
		return nil, err
	}
	st := &ContainerStatus{ContainersNotReady: []ContainerState{}}
	for _, ct := range list {
		if ct.Labels["com.docker.swarm.task.id"] != "" || ct.Labels[ignoreLabel] == "true" {
			continue
		}
		var info struct {
			Name  string
			State struct {
				Status     string
				Running    bool
				Restarting bool
				ExitCode   int
				Health     *struct{ Status string }
			}
			HostConfig struct {
				RestartPolicy struct{ Name string }
			}
		}
		if err := c.dockerGet("/containers/"+url.PathEscape(ct.ID)+"/json", &info); err != nil {
			// Removed between list and inspect.
			continue
		}
		s := ContainerState{
			Name:          strings.TrimPrefix(info.Name, "/"),
			State:         info.State.Status,
			RestartPolicy: info.HostConfig.RestartPolicy.Name,
			ExitCode:      info.State.ExitCode,
		}
		if info.State.Health != nil {
			s.Health = info.State.Health.Status
		}
		if !restartEnabled(s.RestartPolicy) {
			continue
		}
		st.Containers++
		if containerReady(s) {
			st.ContainersReady++
		} else {
			st.ContainersNotReady = append(st.ContainersNotReady, s)
		}
	}
	return st, nil
}

func restartEnabled(policy string) bool {
	switch policy {
	case "always", "unless-stopped", "on-failure":
		return true
	}
	return false
}

// containerReady: running and, with a health check, healthy. An on-failure
// container which exited successfully is done, not broken.
func containerReady(s ContainerState) bool {
	if s.State == "running" {
		return s.Health == "" || s.Health == "healthy"
	}
	return s.RestartPolicy == "on-failure" && s.State == "exited" && s.ExitCode == 0
}
