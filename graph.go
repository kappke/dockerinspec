package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Link is a detected "From talks to To" relation.
type Link struct {
	From, To string // container IDs
	ViaHost  bool   // reached through localhost / host.docker.internal (published port)
	Ports    []int
	Keys     []string // env keys that reference the target
}

var (
	hostPortRe = regexp.MustCompile(`(?i)([a-z0-9][a-z0-9_.\-]*):(\d{1,5})\b`)
	urlHostRe  = regexp.MustCompile(`(?i)://(?:[^/@\s]*@)?([a-z0-9][a-z0-9_.\-]*)`)
)

var hostKeyRe = regexp.MustCompile(`(?i)(HOST|ADDR|SERVER|ENDPOINT|URL|URI|DSN|BROKER)`)

var hostAliases = map[string]bool{
	"localhost": true, "127.0.0.1": true, "0.0.0.0": true, "::1": true,
	"host.docker.internal": true, "gateway.docker.internal": true,
	"host.containers.internal": true, "172.17.0.1": true, "host-gateway": true,
}

// Images of passive infrastructure: they receive connections but never
// initiate them, so env vars they inherit (e.g. a shared env_file) must not
// be read as outgoing links.
var passiveImageRe = regexp.MustCompile(`(?i)(^|/)(postgres|postgis|mysql|mariadb|mongo|redis|valkey|memcached|rabbitmq|elasticsearch|opensearch|clickhouse|cassandra|influxdb|minio|ngrok/ngrok)(:|$|-)`)

func isPassive(c *Container) bool { return passiveImageRe.MatchString(c.Image) }

func sharesNet(a, b *Container) bool {
	for n := range a.Nets {
		if _, ok := b.Nets[n]; ok {
			return true
		}
	}
	return false
}

// buildLinks scans every container's environment for references to other
// containers: either a published host port (localhost:PORT,
// host.docker.internal:PORT) or a resolvable name on a shared network.
func buildLinks(cs []*Container) []*Link {
	byName := map[string][]*Container{}
	byPort := map[int][]*Container{}
	byIP := map[string]*Container{}
	add := func(k string, c *Container) {
		k = strings.ToLower(k)
		for _, x := range byName[k] {
			if x == c {
				return
			}
		}
		byName[k] = append(byName[k], c)
	}
	for _, c := range cs {
		add(c.Name, c)
		if c.Service != "" {
			add(c.Service, c)
		}
		for _, al := range c.Nets {
			for _, a := range al {
				add(a, c)
			}
		}
		for _, p := range c.Ports {
			byPort[p.Host] = append(byPort[p.Host], c)
		}
		for _, ip := range c.IPs {
			byIP[ip] = c
		}
	}

	links := map[string]*Link{}
	order := []string{}
	record := func(from, to *Container, via bool, port int, key string) {
		if from == to {
			return
		}
		id := from.ID + ">" + to.ID + strconv.FormatBool(via)
		l := links[id]
		if l == nil {
			l = &Link{From: from.ID, To: to.ID, ViaHost: via}
			links[id] = l
			order = append(order, id)
		}
		if port > 0 {
			dup := false
			for _, p := range l.Ports {
				dup = dup || p == port
			}
			if !dup {
				l.Ports = append(l.Ports, port)
			}
		}
		dup := false
		for _, k := range l.Keys {
			dup = dup || k == key
		}
		if !dup {
			l.Keys = append(l.Keys, key)
		}
	}

	resolve := func(from *Container, host string, port int, key string) {
		h := strings.ToLower(host)
		if hostAliases[h] {
			if port == 0 {
				return
			}
			for _, t := range byPort[port] {
				record(from, t, true, port, key)
			}
			return
		}
		if t, ok := byIP[h]; ok && sharesNet(from, t) {
			record(from, t, false, port, key)
			return
		}
		for _, t := range byName[h] {
			if t != from && sharesNet(from, t) {
				record(from, t, false, port, key)
			}
		}
	}

	for _, c := range cs {
		if isPassive(c) {
			continue
		}
		for _, kv := range c.Env {
			eq := strings.IndexByte(kv, '=')
			if eq < 0 {
				continue
			}
			key, val := kv[:eq], kv[eq+1:]
			if len(val) < 2 {
				continue
			}
			seen := map[string]bool{}
			for _, m := range hostPortRe.FindAllStringSubmatch(val, -1) {
				port, _ := strconv.Atoi(m[2])
				if port == 0 || port > 65535 {
					continue
				}
				seen[strings.ToLower(m[1])] = true
				resolve(c, m[1], port, key)
			}
			// URLs/hosts without an explicit port (http://api/...)
			for _, m := range urlHostRe.FindAllStringSubmatch(val, -1) {
				if seen[strings.ToLower(m[1])] || hostAliases[strings.ToLower(m[1])] {
					continue
				}
				resolve(c, m[1], 0, key)
			}
			// bare value equal to a service name (DB_HOST=postgres)
			if !strings.ContainsAny(val, "/: ") && len(val) < 64 && hostKeyRe.MatchString(key) {
				if !hostAliases[strings.ToLower(val)] {
					resolve(c, val, 0, key)
				}
			}
		}
	}

	res := make([]*Link, 0, len(order))
	for _, id := range order {
		l := links[id]
		sort.Ints(l.Ports)
		sort.Strings(l.Keys)
		res = append(res, l)
	}
	return res
}
