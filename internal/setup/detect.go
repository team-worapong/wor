package setup

import (
	"context"

	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

func (s Service) detectCommon(ctx context.Context) []Detection {
	results := s.checker.CheckAll(ctx, commonTargets())
	detections := make([]Detection, 0, len(results)+1)
	for _, result := range results {
		detections = append(detections, detectionFromCheck(result))
	}
	detections = append(detections, s.detectIIS())
	return detections
}

func (s Service) redetect(ctx context.Context, name string) Detection {
	for _, target := range commonTargets() {
		if target.Name == name {
			return detectionFromCheck(s.checker.Check(ctx, target))
		}
	}
	return Detection{
		Name:      name,
		Supported: false,
		Status:    string(worRuntime.StatusInfo),
		Message:   "detection target is not configured",
	}
}

func commonTargets() []worRuntime.Target {
	return []worRuntime.Target{
		{
			Name:          "Git",
			Command:       "git",
			VersionArgs:   []string{"version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryCoreTools,
		},
		{
			Name:          "Go",
			Command:       "go",
			VersionArgs:   []string{"version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryCoreTools,
		},
		{
			Name:          "Node.js",
			Command:       "node",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:          "npm",
			Command:       "npm",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:          "PM2",
			Command:       "pm2",
			VersionSource: worRuntime.VersionFromNPMGlobalPackage,
			PackageName:   "pm2",
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:          "PHP",
			Command:       "php",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:          "PHP-FPM",
			Command:       "php-fpm",
			Commands:      []string{"php-fpm", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1", "php-fpm8.0"},
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:          "Python",
			Command:       "python",
			Commands:      []string{"python3", "python"},
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:          "Nginx",
			Command:       "nginx",
			VersionArgs:   []string{"-v"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalWebServers,
		},
		{
			Name:          "Apache",
			Command:       "apache",
			Commands:      []string{"apache2", "httpd"},
			VersionArgs:   []string{"-v"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalWebServers,
		},
		{
			Name:          "Certbot",
			Command:       "certbot",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementOptional,
			Category:      worRuntime.CategoryOptionalRuntimes,
		},
	}
}

func (s Service) detectIIS() Detection {
	if s.system.OS() != "windows" {
		return Detection{
			Name:      "IIS",
			Supported: false,
			Status:    "unsupported",
			Message:   "not supported on this platform",
		}
	}
	return Detection{
		Name:      "IIS",
		Supported: false,
		Status:    "unsupported",
		Message:   "IIS detection is not implemented in phase 1",
	}
}

func detectionFromCheck(result worRuntime.CheckResult) Detection {
	return Detection{
		Name:      result.Name,
		Command:   result.Command,
		Found:     result.Path != "",
		Supported: true,
		Path:      result.Path,
		Version:   result.Version,
		Status:    string(result.Status),
		Message:   result.Message,
	}
}

func findDetection(detections []Detection, name string) Detection {
	for _, detection := range detections {
		if detection.Name == name {
			return detection
		}
	}
	return Detection{Name: name, Supported: false, Status: "unknown"}
}

func replaceDetection(detections []Detection, next Detection) []Detection {
	for i, detection := range detections {
		if detection.Name == next.Name {
			detections[i] = next
			return detections
		}
	}
	return append(detections, next)
}

func webServerDetections(detections []Detection) []Detection {
	return []Detection{
		findDetection(detections, "Nginx"),
		findDetection(detections, "Apache"),
		findDetection(detections, "IIS"),
	}
}
