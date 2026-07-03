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
	HasIssues bool
}

func New(platform worRuntime.Platform) Service {
	return Service{
		checker: worRuntime.NewChecker(platform),
	}
}

func (s Service) Run(ctx context.Context) (Report, error) {
	results := s.checker.CheckAll(ctx, worRuntime.Required())
	return Report{
		Results:   results,
		HasIssues: HasIssues(results),
	}, nil
}

func HasIssues(results []worRuntime.CheckResult) bool {
	for _, result := range results {
		if result.Status != worRuntime.StatusOK {
			return true
		}
	}
	return false
}
