package cliapp

import (
	"fmt"
	"os/exec"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/pm2"
)

func (a *App) cmdInfo(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("target required")
	}
	target := args[0]
	resolved := target
	if !containsSlash(target) {
		if r, ok := a.Store.ResolveHost(target); ok {
			resolved = r
		} else {
			return a.errf("host not found in services.config.json: %s", target)
		}
	}
	domain, service, err := domainmodel.ParseTarget(resolved)
	if err != nil {
		return err
	}
	name := pm2.Name(domain, service)
	serviceDir := a.Store.ServiceDir(domain, service)

	fmt.Fprintln(a.Out, "WOR Info")
	fmt.Fprintln(a.Out, "--------")
	fmt.Fprintf(a.Out, "Target   : %s\n", target)
	fmt.Fprintf(a.Out, "Domain   : %s\n", domain)
	fmt.Fprintf(a.Out, "Service  : %s\n", service)
	fmt.Fprintf(a.Out, "PM2 Name : %s\n", name)
	fmt.Fprintf(a.Out, "Source   : %s\n", serviceDir)
	fmt.Fprintf(a.Out, "Type     : %s\n", a.Store.GetServiceType(domain, service))
	fmt.Fprintln(a.Out, "Hosts    :")
	hosts, _ := a.Store.ListHostsForService(domain, service)
	for _, h := range hosts {
		fmt.Fprintf(a.Out, "  - %s\n", h)
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "PM2:")
	if out, err := pm2.RunCapture("describe", name); err == nil {
		lines := strings.Split(out, "\n")
		if len(lines) > 25 {
			lines = lines[:25]
		}
		fmt.Fprintln(a.Out, strings.Join(lines, "\n"))
	} else {
		fmt.Fprintln(a.Out, "  not running")
	}

	fmt.Fprintln(a.Out)
	if _, err := exec.Command("git", "-C", serviceDir, "rev-parse", "--git-dir").Output(); err == nil {
		fmt.Fprintln(a.Out, "Git:")
		branch, _ := gitOutput(serviceDir, "branch", "--show-current")
		commit, _ := gitOutput(serviceDir, "rev-parse", "--short", "HEAD")
		statusOut, _ := gitOutput(serviceDir, "status", "--short")
		changed := 0
		if statusOut != "" {
			changed = len(strings.Split(statusOut, "\n"))
		}
		fmt.Fprintf(a.Out, "  Branch : %s\n", branch)
		fmt.Fprintf(a.Out, "  Commit : %s\n", commit)
		fmt.Fprintf(a.Out, "  Status : %d changed files\n", changed)
	}
	return nil
}
