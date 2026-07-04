package cli

import (
	"github.com/team-worapong/wor/internal/domain"
	"github.com/team-worapong/wor/internal/engine"
	"github.com/team-worapong/wor/internal/output"
	"github.com/team-worapong/wor/internal/service"
)

func domainAddedView(metadata domain.Metadata) output.DomainAdded {
	return output.DomainAdded{
		Domain:    metadata.DomainName,
		Path:      metadata.DomainPath,
		CreatedAt: metadata.CreatedAt,
		Existing:  metadata.Existing,
	}
}

func domainListView(items []domain.Metadata) []output.DomainListItem {
	view := make([]output.DomainListItem, 0, len(items))
	for _, item := range items {
		view = append(view, output.DomainListItem{
			Domain:    item.DomainName,
			Path:      item.DomainPath,
			CreatedAt: item.CreatedAt,
		})
	}
	return view
}

func domainDetailsView(report engine.DomainReport) output.DomainDetails {
	services := make([]string, 0, len(report.Services))
	for _, item := range report.Services {
		services = append(services, item.FQDN)
	}
	return output.DomainDetails{
		Domain:    report.Domain.DomainName,
		Path:      report.Domain.DomainPath,
		CreatedAt: report.Domain.CreatedAt,
		Services:  services,
	}
}

func serviceAddedView(metadata service.Metadata) output.ServiceAdded {
	return output.ServiceAdded{
		FQDN:             metadata.FQDN,
		Domain:           metadata.DomainName,
		Template:         metadata.ServiceTemplate,
		ApplicationRoute: metadata.ApplicationRoute,
		PublicPath:       metadata.PublicPath,
		ServicePath:      metadata.ServicePath,
		CreatedAt:        metadata.CreatedAt,
	}
}

func serviceListView(items []service.Metadata) []output.ServiceListItem {
	view := make([]output.ServiceListItem, 0, len(items))
	for _, item := range items {
		view = append(view, output.ServiceListItem{
			FQDN:             item.FQDN,
			Domain:           item.DomainName,
			Template:         item.ServiceTemplate,
			ApplicationRoute: item.ApplicationRoute,
		})
	}
	return view
}

func serviceDetailsView(report engine.ServiceReport) output.ServiceDetails {
	return output.ServiceDetails{
		FQDN:                report.Service.FQDN,
		Template:            report.Service.ServiceTemplate,
		RuntimeRequirements: report.RuntimeRequirements,
		ProcessRequirements: report.ProcessRequirements,
		ApplicationRoute:    report.Service.ApplicationRoute,
		PublicPath:          report.Service.PublicPath,
		ServicePath:         report.Service.ServicePath,
		CreatedAt:           report.Service.CreatedAt,
	}
}
