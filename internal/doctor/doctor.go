package doctor

import (
	"context"

	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

type Service struct {
	checker worRuntime.Checker
}

type Report struct {
	Results   []worRuntime.CheckResult
	Groups    []Group
	HasIssues bool
}

type Group struct {
	Category    worRuntime.Category
	Title       string
	Requirement worRuntime.Requirement
	Results     []worRuntime.CheckResult
}

func New(platform worRuntime.Platform) Service {
	return Service{
		checker: worRuntime.NewChecker(platform),
	}
}

func (s Service) Run(ctx context.Context) (Report, error) {
	results := s.checker.CheckAll(ctx, worRuntime.DefaultTargets())
	return Report{
		Results:   results,
		Groups:    GroupResults(results),
		HasIssues: HasIssues(results),
	}, nil
}

func HasIssues(results []worRuntime.CheckResult) bool {
	for _, result := range results {
		if result.IsIssue() {
			return true
		}
	}
	return false
}

func GroupResults(results []worRuntime.CheckResult) []Group {
	byCategory := make(map[worRuntime.Category][]worRuntime.CheckResult)
	for _, result := range results {
		byCategory[result.Category] = append(byCategory[result.Category], result)
	}

	order := []worRuntime.Category{
		worRuntime.CategoryCoreTools,
		worRuntime.CategoryOptionalRuntimes,
		worRuntime.CategoryOptionalWebServers,
	}

	groups := make([]Group, 0, len(order))
	for _, category := range order {
		groupResults := byCategory[category]
		if len(groupResults) == 0 {
			continue
		}
		groups = append(groups, Group{
			Category:    category,
			Title:       groupTitle(category),
			Requirement: groupResults[0].Requirement,
			Results:     groupResults,
		})
	}
	return groups
}

func groupTitle(category worRuntime.Category) string {
	switch category {
	case worRuntime.CategoryCoreTools:
		return "Core tools"
	case worRuntime.CategoryOptionalRuntimes:
		return "Optional runtimes"
	case worRuntime.CategoryOptionalWebServers:
		return "Optional web servers"
	default:
		return "Other checks"
	}
}
