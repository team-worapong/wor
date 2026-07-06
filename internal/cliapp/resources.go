package cliapp

// Live host + per-service cpu/mem reporting for `wor health` and `wor
// info` (owner request 2026-07-07, layout B: a separate "Resources"
// section so the existing status lines don't change shape, plain text,
// no colors/thresholds).
//
// Sources per provider -- no new dependencies, no sudo:
//   - pm2:       pm2 jlist's own live monit.cpu/monit.memory (already
//                parsed into pm2.ProcessInfo).
//   - systemd:   systemd.GetInfoBatch (CPUUsageNSec delta over its
//                internal 200ms sampling window + MemoryCurrent).
//   - php pool:  sum over the pool's worker processes
//                (phpfpm.PoolWorkerPIDs) from /proc/<pid>/stat &
//                /proc/<pid>/status, sampled over the same window.
//   - host:      /proc/stat + /proc/meminfo.
//   - static:    no process, rendered as "-".
//
// The /proc readers are Linux-only (resources_linux.go, with stubs in
// resources_stub.go): Linux is the production target. On macOS the
// section still renders whatever pm2 reports; host/php rows show "-".
//
// CPU% convention matches top and the existing pm2/systemd numbers:
// 100% = one core fully busy (per-service), while the Host line's CPU%
// is of TOTAL capacity across all cores (also top's convention for the
// summary line). Service mem% is RSS against the host's MemTotal.

import (
	"fmt"
	"time"

	"wor/internal/domainmodel"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/systemd"
)

type svcUsage struct {
	cpuKnown bool
	cpuPct   float64
	memKnown bool
	memBytes int64
}

type hostUsage struct {
	cpuKnown bool
	cpuPct   float64
	cores    int
	memKnown bool
	memTotal int64 // bytes
	memUsed  int64 // bytes (MemTotal - MemAvailable)
}

// resourceSampleInterval is the fallback sampling window when there are
// no systemd services (whose GetInfoBatch already sleeps 200ms and
// serves as the shared window otherwise). The real elapsed time is
// measured with a wall clock either way, so the math never assumes the
// nominal value.
const resourceSampleInterval = 250 * time.Millisecond

// collectResources gathers live usage for the host and every ref,
// keyed by "domain/service". pm2Procs may be nil (no pm2 services or
// pm2 unavailable); pm2 entries then simply come back unknown.
func (a *App) collectResources(refs []domainmodel.ServiceRef, pm2Procs map[string]pm2.ProcessInfo) (hostUsage, map[string]svcUsage) {
	usage := map[string]svcUsage{}

	var sysRefs []systemd.Ref
	phpPIDs := map[string][]int{}
	for _, ref := range refs {
		target := ref.Domain + "/" + ref.Service.Name
		switch domainmodel.ProcessProviderFor(ref.Service.Type) {
		case "pm2":
			if info, ok := pm2Procs[pm2.Name(ref.Domain, ref.Service.Name)]; ok && info.Status == "online" {
				usage[target] = svcUsage{cpuKnown: true, cpuPct: info.CPUPercent, memKnown: true, memBytes: info.MemoryBytes}
			}
		case "systemd":
			sysRefs = append(sysRefs, systemd.Ref{Domain: ref.Domain, Service: ref.Service.Name})
		default:
			if ref.Service.UsesPerServicePHPFPM() {
				if pids := phpfpm.PoolWorkerPIDs(ref.Domain, ref.Service.Name); len(pids) > 0 {
					phpPIDs[target] = pids
				}
			}
		}
	}

	// First samples, then ONE shared wait (GetInfoBatch's internal
	// sleep doubles as it when systemd services exist), then second
	// samples -- the whole report pays the sampling latency once, no
	// matter how many services are involved.
	host1, host1OK := hostCPUSample()
	php1 := map[string]uint64{}
	for t, pids := range phpPIDs {
		php1[t] = pidsCPUTicks(pids)
	}
	start := time.Now()
	var sysInfo map[systemd.Ref]systemd.Info
	if len(sysRefs) > 0 {
		sysInfo = systemd.GetInfoBatch(sysRefs)
	} else if host1OK || len(phpPIDs) > 0 {
		time.Sleep(resourceSampleInterval)
	}
	elapsed := time.Since(start)

	var host hostUsage
	if host1OK {
		if host2, ok := hostCPUSample(); ok {
			host.cpuKnown, host.cpuPct, host.cores = hostCPUFromSamples(host1, host2)
		}
	}
	if total, avail, ok := hostMemInfo(); ok && total > 0 {
		host.memKnown, host.memTotal, host.memUsed = true, total, total-avail
	}

	for t, pids := range phpPIDs {
		var u svcUsage
		if rss := pidsRSSBytes(pids); rss > 0 {
			u.memKnown, u.memBytes = true, rss
		}
		if ticks2 := pidsCPUTicks(pids); ticks2 >= php1[t] && elapsed > 0 {
			u.cpuKnown = true
			u.cpuPct = ticksToCPUPercent(ticks2-php1[t], elapsed)
		}
		if u.cpuKnown || u.memKnown {
			usage[t] = u
		}
	}
	for ref, info := range sysInfo {
		if !info.Active {
			continue
		}
		usage[ref.Domain+"/"+ref.Service] = svcUsage{
			cpuKnown: info.CPUKnown, cpuPct: info.CPUPercent,
			memKnown: info.MemKnown, memBytes: info.MemoryBytes,
		}
	}
	return host, usage
}

// renderResourcesSection prints the "Resources" block (layout B):
//
//	Resources
//	---------------------------------
//	  Host                  CPU 12% (4 cores)   Mem 1.9G / 3.8G (50%)
//	  com-moodasoft/app     cpu 0.3%            mem 66.2M (1.7%)
//	  default/web           -                   -
//
// Skipped entirely when nothing measurable was collected (e.g. on a
// platform with no /proc and no pm2 services).
func (a *App) renderResourcesSection(host hostUsage, usage map[string]svcUsage, refs []domainmodel.ServiceRef) {
	if !host.cpuKnown && !host.memKnown && len(usage) == 0 {
		return
	}
	width := len("Host")
	for _, ref := range refs {
		if l := len(ref.Domain) + 1 + len(ref.Service.Name); l > width {
			width = l
		}
	}

	hostCPU, hostMem := "-", "-"
	if host.cpuKnown {
		hostCPU = fmt.Sprintf("CPU %.0f%% (%d cores)", host.cpuPct, host.cores)
	}
	if host.memKnown {
		hostMem = fmt.Sprintf("Mem %s / %s (%.0f%%)", formatMemBytes(host.memUsed), formatMemBytes(host.memTotal),
			float64(host.memUsed)/float64(host.memTotal)*100)
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Resources")
	fmt.Fprintln(a.Out, "---------------------------------")
	fmt.Fprintf(a.Out, "  %-*s  %-20s  %s\n", width, "Host", hostCPU, hostMem)
	for _, ref := range refs {
		target := ref.Domain + "/" + ref.Service.Name
		cpu, mem := "-", "-"
		if u, ok := usage[target]; ok {
			if u.cpuKnown {
				cpu = fmt.Sprintf("cpu %.1f%%", u.cpuPct)
			}
			if u.memKnown {
				mem = fmt.Sprintf("mem %s", formatMemBytes(u.memBytes))
				if host.memKnown {
					mem += fmt.Sprintf(" (%.1f%%)", float64(u.memBytes)/float64(host.memTotal)*100)
				}
			}
		}
		fmt.Fprintf(a.Out, "  %-*s  %-20s  %s\n", width, target, cpu, mem)
	}
}

// formatMemBytes renders bytes the way admins read memory: one decimal
// in M below 1G, one decimal in G from there up. Named to avoid
// colliding with other formatters in this package.
func formatMemBytes(b int64) string {
	const (
		mb = 1024 * 1024
		gb = 1024 * mb
	)
	if b >= gb {
		return fmt.Sprintf("%.1fG", float64(b)/float64(gb))
	}
	return fmt.Sprintf("%.1fM", float64(b)/float64(mb))
}
