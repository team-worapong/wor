//go:build windows

package cliapp

// checkWorHomeReachability/webServerRunUser are no-ops on Windows: the
// unix owner/group/other + traverse-permission model this check is
// built around doesn't apply there (NTFS ACLs work completely
// differently), and wor's own host providers don't hit this class of
// problem on Windows. See permcheck_unix.go for the real
// implementation.
func webServerRunUser(provider string) string { return "" }

func webUserExists(name string) bool { return false }

func socketDeniesUser(path, username string) bool { return false }

func unixOwnerGroupMode(path string) string { return "" }

func checkWorHomeReachability(a *App, webUser string) []string { return nil }

func checkServiceReachability(a *App, webUser, domain, service, svcType string) []string {
	return nil
}

func worHomeReachabilityFixCommand(webUser string, blocked []string) string { return "" }
