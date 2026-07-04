package dbbackup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/osutil"
)

// toolCandidates returns (PATH names, extra fallback paths) for a given
// client/dump tool id, covering Linux, macOS Homebrew, and Windows
// (Program Files / Chocolatey) install conventions.
func toolCandidates(id string) (names []string, extra []string) {
	pf := envOr("ProgramFiles", `C:\Program Files`)
	choco := `C:\ProgramData\chocolatey\bin`

	switch id {
	case "mysql-client":
		if osutil.IsWindows() {
			return []string{"mysql.exe", "mysql"}, []string{filepath.Join(pf, "MySQL", "MySQL Server 8.0", "bin", "mysql.exe"), filepath.Join(choco, "mysql.exe")}
		}
		return []string{"mysql", "mariadb"}, []string{
			"/usr/bin/mysql", "/usr/sbin/mysql", "/usr/local/bin/mysql", "/usr/local/mysql/bin/mysql", "/opt/homebrew/bin/mysql",
			"/usr/bin/mariadb", "/usr/local/bin/mariadb", "/opt/homebrew/bin/mariadb",
		}
	case "mysql-dump":
		if osutil.IsWindows() {
			return []string{"mysqldump.exe", "mysqldump"}, []string{filepath.Join(pf, "MySQL", "MySQL Server 8.0", "bin", "mysqldump.exe"), filepath.Join(choco, "mysqldump.exe")}
		}
		return []string{"mysqldump", "mariadb-dump", "mysqlpump"}, []string{
			"/usr/bin/mysqldump", "/usr/sbin/mysqldump", "/usr/local/bin/mysqldump", "/usr/local/mysql/bin/mysqldump", "/opt/homebrew/bin/mysqldump",
			"/usr/bin/mariadb-dump", "/usr/local/bin/mariadb-dump", "/opt/homebrew/bin/mariadb-dump",
			"/usr/bin/mysqlpump", "/usr/local/bin/mysqlpump", "/opt/homebrew/bin/mysqlpump",
		}
	case "postgres-client":
		if osutil.IsWindows() {
			return []string{"psql.exe", "psql"}, []string{filepath.Join(pf, "PostgreSQL", "16", "bin", "psql.exe"), filepath.Join(choco, "psql.exe")}
		}
		return []string{"psql"}, []string{"/usr/bin/psql", "/usr/local/bin/psql", "/opt/homebrew/bin/psql"}
	case "postgres-dump":
		if osutil.IsWindows() {
			return []string{"pg_dump.exe", "pg_dump"}, []string{filepath.Join(pf, "PostgreSQL", "16", "bin", "pg_dump.exe"), filepath.Join(choco, "pg_dump.exe")}
		}
		return []string{"pg_dump"}, []string{"/usr/bin/pg_dump", "/usr/local/bin/pg_dump", "/opt/homebrew/bin/pg_dump"}
	case "sqlserver-client":
		if osutil.IsWindows() {
			return []string{"sqlcmd.exe", "sqlcmd"}, []string{filepath.Join(pf, "Microsoft SQL Server", "Client SDK", "ODBC", "170", "Tools", "Binn", "SQLCMD.EXE")}
		}
		return []string{"sqlcmd"}, []string{
			"/opt/mssql-tools18/bin/sqlcmd", "/opt/mssql-tools/bin/sqlcmd", "/usr/bin/sqlcmd", "/usr/local/bin/sqlcmd", "/opt/homebrew/bin/sqlcmd",
		}
	case "sqlserver-dump":
		if osutil.IsWindows() {
			return []string{"sqlpackage.exe", "sqlpackage"}, []string{filepath.Join(pf, "Microsoft SQL Server", "160", "DAC", "bin", "sqlpackage.exe")}
		}
		return []string{"sqlpackage"}, []string{"/usr/bin/sqlpackage", "/usr/local/bin/sqlpackage", "/opt/homebrew/bin/sqlpackage"}
	case "sqlite-client":
		if osutil.IsWindows() {
			return []string{"sqlite3.exe", "sqlite3"}, []string{filepath.Join(choco, "sqlite3.exe")}
		}
		return []string{"sqlite3"}, []string{"/usr/bin/sqlite3", "/usr/local/bin/sqlite3", "/opt/homebrew/bin/sqlite3"}
	default:
		return nil, nil
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func findTool(id string) (string, bool) {
	names, extra := toolCandidates(id)
	for _, n := range names {
		if p := osutil.Which(n); p != "" {
			return p, true
		}
	}
	for _, p := range extra {
		if osutil.IsExecutableFile(p) {
			return p, true
		}
	}
	return "", false
}

// ClientBin returns the client binary for engine, if any is installed.
func ClientBin(engine string) (string, bool) {
	switch engine {
	case "mysql", "mariadb":
		return findTool("mysql-client")
	case "postgresql":
		return findTool("postgres-client")
	case "sqlserver":
		return findTool("sqlserver-client")
	case "sqlite":
		return findTool("sqlite-client")
	default:
		return "", false
	}
}

// DumpBin returns the dump/export tool for engine, if any is installed.
func DumpBin(engine string) (string, bool) {
	switch engine {
	case "mysql", "mariadb":
		return findTool("mysql-dump")
	case "postgresql":
		return findTool("postgres-dump")
	case "sqlserver":
		return findTool("sqlserver-dump")
	case "sqlite":
		return findTool("sqlite-client")
	default:
		return "", false
	}
}

// DetectEngine best-effort guesses an installed engine when a profile
// doesn't specify one, matching lib/config.sh database_detect_engine().
func DetectEngine() string {
	if bin, ok := findTool("mysql-client"); ok {
		out, _ := exec.Command(bin, "--version").CombinedOutput()
		if strings.Contains(strings.ToLower(string(out)), "mariadb") {
			return "MariaDB"
		}
		return "MySQL"
	}
	if _, ok := findTool("postgres-client"); ok {
		return "PostgreSQL"
	}
	if _, ok := findTool("sqlserver-client"); ok {
		return "SQL Server"
	}
	if _, ok := findTool("sqlite-client"); ok {
		return "SQLite"
	}
	return "Unknown"
}
