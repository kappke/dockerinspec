package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PortMap struct {
	Host  int
	Cont  int
	Proto string
}

type Container struct {
	ID        string
	Name      string
	Image     string
	State     string // running, exited, paused, restarting...
	Health    string
	StartedAt time.Time
	EndedAt   time.Time
	Project   string
	Service   string
	Ports     []PortMap // published ports (deduped v4/v6)
	Exposed   []int
	Env       []string
	Nets      map[string][]string // network -> aliases
	IPs       []string
	HostNet   bool
}

type inspectDoc struct {
	ID     string `json:"Id"`
	Name   string
	Config struct {
		Image  string
		Env    []string
		Labels map[string]string
	}
	State struct {
		Status     string
		StartedAt  string
		FinishedAt string
		Health     *struct{ Status string }
	}
	HostConfig struct {
		NetworkMode string
	}
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string
		}
		Networks map[string]struct {
			Aliases   []string
			IPAddress string
		}
	}
}

func dockerOut(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func fetchContainers() ([]*Container, error) {
	ids, err := dockerOut(10*time.Second, "ps", "-aq", "--no-trunc")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(ids))
	if len(fields) == 0 {
		return nil, nil
	}
	raw, err := dockerOut(15*time.Second, append([]string{"inspect"}, fields...)...)
	if err != nil {
		return nil, err
	}
	var docs []inspectDoc
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, err
	}
	var res []*Container
	for _, d := range docs {
		c := &Container{
			ID:      d.ID,
			Name:    strings.TrimPrefix(d.Name, "/"),
			Image:   d.Config.Image,
			State:   d.State.Status,
			Env:     d.Config.Env,
			Project: d.Config.Labels["com.docker.compose.project"],
			Service: d.Config.Labels["com.docker.compose.service"],
			Nets:    map[string][]string{},
			HostNet: d.HostConfig.NetworkMode == "host",
		}
		if d.State.Health != nil {
			c.Health = d.State.Health.Status
		}
		c.StartedAt, _ = time.Parse(time.RFC3339Nano, d.State.StartedAt)
		c.EndedAt, _ = time.Parse(time.RFC3339Nano, d.State.FinishedAt)
		seen := map[string]bool{}
		for k, binds := range d.NetworkSettings.Ports {
			parts := strings.SplitN(k, "/", 2)
			cp, _ := strconv.Atoi(parts[0])
			proto := "tcp"
			if len(parts) == 2 {
				proto = parts[1]
			}
			if len(binds) == 0 {
				c.Exposed = append(c.Exposed, cp)
				continue
			}
			for _, b := range binds {
				hp, _ := strconv.Atoi(b.HostPort)
				key := fmt.Sprintf("%d:%d/%s", hp, cp, proto)
				if hp == 0 || seen[key] {
					continue
				}
				seen[key] = true
				c.Ports = append(c.Ports, PortMap{hp, cp, proto})
			}
		}
		sort.Slice(c.Ports, func(i, j int) bool { return c.Ports[i].Host < c.Ports[j].Host })
		sort.Ints(c.Exposed)
		for n, v := range d.NetworkSettings.Networks {
			c.Nets[n] = v.Aliases
			if v.IPAddress != "" {
				c.IPs = append(c.IPs, v.IPAddress)
			}
		}
		res = append(res, c)
	}
	return res, nil
}

func (c *Container) Running() bool { return c.State == "running" }

func humanDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// StatusText is a short human status such as "up 23h · healthy".
func (c *Container) StatusText() string {
	switch c.State {
	case "running":
		s := "up " + humanDur(time.Since(c.StartedAt))
		if c.Health != "" && c.Health != "none" {
			s += " " + c.Health
		}
		return s
	case "exited", "dead":
		if !c.EndedAt.IsZero() && c.EndedAt.Year() > 2000 {
			return "exited " + humanDur(time.Since(c.EndedAt)) + " ago"
		}
	}
	return c.State
}

func (c *Container) PortsText() string {
	var p []string
	for _, pm := range c.Ports {
		if pm.Host == pm.Cont {
			p = append(p, strconv.Itoa(pm.Host))
		} else {
			p = append(p, fmt.Sprintf("%d→%d", pm.Host, pm.Cont))
		}
	}
	return strings.Join(p, " ")
}

func (c *Container) Group() string {
	if c.Project == "" {
		return "(standalone)"
	}
	return c.Project
}

// containerAction runs `docker <verb> <id>` (start, stop, restart, rm).
func containerAction(verb, id string) error {
	args := []string{verb}
	if verb == "rm" {
		args = append(args, "-f") // also removes a running container
	}
	_, err := dockerOut(45*time.Second, append(args, id)...)
	return err
}
