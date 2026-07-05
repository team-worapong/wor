// Package dbbackup implements `wor database backup`, porting the
// database engine detection in lib/config.sh and the backup dispatch in
// commands/service.sh cmd_database(). wor intentionally supports backup
// only -- restore, drop, and migration are out of scope by design (see
// the shell CLI's README "Principles" section), and the Go rewrite
// keeps that boundary.
package dbbackup

import (
	"path/filepath"

	"wor/internal/config"
)

// Profile is one $WOR_HOME/configs/database/<profile>.env file.
type Profile struct {
	Engine                 string
	Host                   string
	Port                   string
	Name                   string
	User                   string
	Pass                   string
	Charset                string
	Path                   string // SQLite only
	SSLMode                string // PostgreSQL only
	Encrypt                string // SQL Server only
	TrustServerCertificate string // SQL Server only
}

// ProfilePath returns $WOR_HOME/configs/database/<profile>.env.
func ProfilePath(configsDir, profile string) string {
	return filepath.Join(configsDir, "database", profile+".env")
}

// LoadProfile reads a profile's .env file (same key=value format as
// ~/.wor/config, reusing internal/config's parser).
func LoadProfile(path string) (*Profile, error) {
	m, err := config.ParseKV(path)
	if err != nil {
		return nil, err
	}
	engine := m["DB_ENGINE"]
	if engine == "" {
		engine = m["DB_TYPE"]
	}
	if engine == "" {
		engine = "mysql"
	}
	return &Profile{
		Engine:                 NormalizeEngine(engine),
		Host:                   orDefault(m["DB_HOST"], "127.0.0.1"),
		Port:                   m["DB_PORT"],
		Name:                   m["DB_NAME"],
		User:                   m["DB_USER"],
		Pass:                   m["DB_PASS"],
		Charset:                orDefault(m["DB_CHARSET"], "utf8mb4"),
		Path:                   m["DB_PATH"],
		SSLMode:                orDefault(m["DB_SSLMODE"], "prefer"),
		Encrypt:                m["DB_ENCRYPT"],
		TrustServerCertificate: orDefault(m["DB_TRUST_SERVER_CERTIFICATE"], "true"),
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// DefaultProfileEnv is the scaffold written by `wor database add`,
// matching commands/service.sh cmd_database()'s add branch.
const DefaultProfileEnv = `# Supported engines: mysql, mariadb, postgresql, sqlserver, sqlite
DB_ENGINE=mysql
# Backward-compatible alias. DB_ENGINE takes precedence.
DB_TYPE=mysql

DB_HOST=127.0.0.1
DB_PORT=3306
DB_NAME=
DB_USER=backup
DB_PASS=
DB_CHARSET=utf8mb4

# SQLite only
DB_PATH=

# PostgreSQL optional
DB_SSLMODE=prefer

# SQL Server optional
DB_ENCRYPT=false
DB_TRUST_SERVER_CERTIFICATE=true
`

// NormalizeEngine mirrors lib/config.sh normalize_db_engine().
func NormalizeEngine(v string) string {
	switch v {
	case "mysql", "MYSQL", "Mysql":
		return "mysql"
	case "mariadb", "maria-db", "MariaDB":
		return "mariadb"
	case "postgres", "postgresql", "pgsql", "PostgreSQL":
		return "postgresql"
	case "mssql", "sqlserver", "sql-server", "SQLServer":
		return "sqlserver"
	case "sqlite", "sqlite3", "SQLite":
		return "sqlite"
	default:
		return v
	}
}
