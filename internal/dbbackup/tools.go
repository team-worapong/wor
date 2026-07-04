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
	// The remaining ids are used only by `wor doctor`'s Database
	// checklist, which wants MySQL/MariaDB/Redis reported as distinct
	// rows -- unlike ClientBin/DumpBin above, which deliberately treat
	// "mysql-client" as either MySQL or MariaDB since backup/restore
	// logic doesn't care which one is actually running.
	case "mysql-client-only":
		if osutil.IsWindows() {
			return []string{"mysql.exe", "mysql"}, []string{filepath.Join(pf, "MySQL", "MySQL Server 8.0", "bin", "mysql.exe"), filepath.Join(choco, "mysql.exe")}
		}
		return []string{"mysql"}, []string{"/usr/bin/mysql", "/usr/sbin/mysql", "/usr/local/bin/mysql", "/usr/local/mysql/bin/mysql", "/opt/homebrew/bin/mysql"}
	case "mysql-server":
		if osutil.IsWindows() {
			return []string{"mysqld.exe", "mysqld"}, []string{filepath.Join(pf, "MySQL", "MySQL Server 8.0", "bin", "mysqld.exe")}
		}
		return []string{"mysqld"}, []string{"/usr/sbin/mysqld", "/usr/bin/mysqld", "/usr/local/bin/mysqld", "/usr/local/mysql/bin/mysqld", "/opt/homebrew/bin/mysqld", "/opt/homebrew/opt/mysql/bin/mysqld"}
	case "mariadb-client":
		if osutil.IsWindows() {
			return []string{"mariadb.exe", "mariadb"}, []string{filepath.Join(choco, "mariadb.exe")}
		}
		return []string{"mariadb"}, []string{"/usr/bin/mariadb", "/usr/local/bin/mariadb", "/opt/homebrew/bin/mariadb"}
	case "mariadb-server":
		if osutil.IsWindows() {
			return []string{"mariadbd.exe", "mariadbd"}, []string{filepath.Join(choco, "mariadbd.exe")}
		}
		return []string{"mariadbd"}, []string{"/usr/sbin/mariadbd", "/usr/bin/mariadbd", "/usr/local/bin/mariadbd", "/opt/homebrew/bin/mariadbd", "/opt/homebrew/opt/mariadb/bin/mariadbd"}
	case "redis-server":
		if osutil.IsWindows() {
			return []string{"redis-server.exe", "redis-server"}, []string{filepath.Join(choco, "redis-server.exe")}
		}
		return []string{"redis-server"}, []string{"/usr/bin/redis-server", "/usr/local/bin/redis-server", "/opt/homebrew/bin/redis-server"}
	case "redis-client":
		if osutil.IsWindows() {
			return []string{"redis-cli.exe", "redis-cli"}, []string{filepath.Join(choco, "redis-cli.exe")}
		}
		return []string{"redis-cli"}, []string{"/usr/bin/redis-cli", "/usr/local/bin/redis-cli", "/opt/homebrew/bin/redis-cli"}
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

// MySQLClientBin returns the MySQL client binary, strictly -- unlike
// ClientBin("mysql"), this does not also match a "mariadb" binary, so
// `wor doctor` can report MySQL and MariaDB as separate rows.
func MySQLClientBin() (string, bool) { return findTool("mysql-client-only") }

// MySQLServerBin returns the mysqld server binary, if installed.
func MySQLServerBin() (string, bool) { return findTool("mysql-server") }

// MariaDBBin returns the mariadbd server binary if installed, else the
// mariadb client binary -- either indicates MariaDB is present.
func MariaDBBin() (string, bool) {
	if bin, ok := findTool("mariadb-server"); ok {
		return bin, true
	}
	return findTool("mariadb-client")
}

// RedisBin returns the redis-server binary if installed, else
// redis-cli -- either indicates Redis is present.
func RedisBin() (string, bool) {
	if bin, ok := findTool("redis-server"); ok {
		return bin, true
	}
	return findTool("redis-client")
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
