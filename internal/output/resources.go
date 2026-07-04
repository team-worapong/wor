package output

import (
	"fmt"
	"strings"
)

type DomainListItem struct {
	Domain    string
	Path      string
	CreatedAt string
}

type DomainDetails struct {
	Domain    string
	Path      string
	CreatedAt string
	Services  []string
}

type ServiceListItem struct {
	FQDN             string
	Domain           string
	Template         string
	ApplicationRoute string
}

type ServiceDetails struct {
	FQDN                string
	Template            string
	RuntimeRequirements []string
	ProcessRequirements []string
	ApplicationRoute    string
	PublicPath          string
	ServicePath         string
	CreatedAt           string
}

type DomainAdded struct {
	Domain    string
	Path      string
	CreatedAt string
	Existing  bool
}

type ServiceAdded struct {
	FQDN             string
	Domain           string
	Template         string
	ApplicationRoute string
	PublicPath       string
	ServicePath      string
	CreatedAt        string
}

func (r *Renderer) RenderDomainList(items []DomainListItem) {
	if len(items) == 0 {
		r.Text("No domains found.")
		return
	}

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Domain, item.Path, item.CreatedAt})
	}
	r.Table([]string{"Domain", "Path", "Created At"}, rows)
}

func (r *Renderer) RenderDomain(details DomainDetails) {
	r.Text("Domain")
	r.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"Domain", details.Domain},
			{"Path", details.Path},
			{"Created At", details.CreatedAt},
			{"Services", fmt.Sprintf("%d", len(details.Services))},
		},
	)

	r.Text("")
	r.Text("Services")
	if len(details.Services) == 0 {
		r.Text("(none)")
		return
	}
	rows := make([][]string, 0, len(details.Services))
	for _, fqdn := range details.Services {
		rows = append(rows, []string{fqdn})
	}
	r.Table([]string{"FQDN"}, rows)
}

func (r *Renderer) RenderServiceList(items []ServiceListItem) {
	if len(items) == 0 {
		r.Text("No services found.")
		return
	}

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.FQDN,
			item.Domain,
			item.Template,
			valueOrDash(item.ApplicationRoute),
		})
	}
	r.Table([]string{"FQDN", "Domain", "Template", "Application Route"}, rows)
}

func (r *Renderer) RenderService(details ServiceDetails) {
	r.Text("Service")
	r.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"FQDN", details.FQDN},
			{"Service Template", details.Template},
			{"Runtime Requirements", joinOrNone(details.RuntimeRequirements)},
			{"Process Requirements", joinOrNone(details.ProcessRequirements)},
			{"Application Route", valueOrDash(details.ApplicationRoute)},
			{"Public Path", details.PublicPath},
			{"Service Path", details.ServicePath},
			{"Created At", details.CreatedAt},
		},
	)
}

func (r *Renderer) RenderDomainAdded(item DomainAdded) {
	if item.Existing {
		r.Warning("domain already exists")
	} else {
		r.Success("domain added")
	}
	r.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"Domain", item.Domain},
			{"Path", item.Path},
			{"Created At", item.CreatedAt},
		},
	)
}

func (r *Renderer) RenderServiceAdded(item ServiceAdded) {
	r.Success("service added")
	rows := [][]string{
		{"FQDN", item.FQDN},
		{"Domain", item.Domain},
		{"Template", item.Template},
	}
	if strings.TrimSpace(item.ApplicationRoute) != "" {
		rows = append(rows, []string{"Application Route", item.ApplicationRoute})
	}
	rows = append(rows,
		[]string{"Public Path", item.PublicPath},
		[]string{"Service Path", item.ServicePath},
		[]string{"Created At", item.CreatedAt},
	)
	r.Table([]string{"Key", "Value"}, rows)
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
