package doctor

import (
	"testing"

	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

func TestHasIssuesOnlyCountsRequiredChecks(t *testing.T) {
	t.Parallel()

	results := []worRuntime.CheckResult{
		{
			Name:        "PHP",
			Status:      worRuntime.StatusInfo,
			Requirement: worRuntime.RequirementOptional,
			Category:    worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:        "Go",
			Status:      worRuntime.StatusOK,
			Requirement: worRuntime.RequirementRequired,
			Category:    worRuntime.CategoryCoreTools,
		},
	}

	if HasIssues(results) {
		t.Fatal("optional missing checks should not count as required issues")
	}

	results[1].Status = worRuntime.StatusIssue
	if !HasIssues(results) {
		t.Fatal("missing required checks should count as required issues")
	}
}

func TestGroupResultsKeepsDoctorSections(t *testing.T) {
	t.Parallel()

	groups := GroupResults([]worRuntime.CheckResult{
		{
			Name:        "Go",
			Status:      worRuntime.StatusOK,
			Requirement: worRuntime.RequirementRequired,
			Category:    worRuntime.CategoryCoreTools,
		},
		{
			Name:        "Node.js",
			Status:      worRuntime.StatusInfo,
			Requirement: worRuntime.RequirementOptional,
			Category:    worRuntime.CategoryOptionalRuntimes,
		},
		{
			Name:        "Nginx",
			Status:      worRuntime.StatusInfo,
			Requirement: worRuntime.RequirementOptional,
			Category:    worRuntime.CategoryOptionalWebServers,
		},
	})

	if len(groups) != 3 {
		t.Fatalf("len(groups) = %d", len(groups))
	}
	if groups[0].Title != "Core tools" {
		t.Fatalf("groups[0].Title = %q", groups[0].Title)
	}
	if groups[1].Title != "Optional runtimes" {
		t.Fatalf("groups[1].Title = %q", groups[1].Title)
	}
	if groups[2].Title != "Optional web servers" {
		t.Fatalf("groups[2].Title = %q", groups[2].Title)
	}
}
