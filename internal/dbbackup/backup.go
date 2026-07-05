package dbbackup

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Result is one completed backup file.
type Result struct {
	Database string
	Path     string
}

// Options carries everything Backup needs beyond the loaded profile.
type Options struct {
	Domain        string
	Profile       string
	BackupsDir    string // $WOR_HOME/backups
	Database      string // explicit database name (optional; empty = use profile default or list all)
	RetentionDays int
}

// Backup dispatches to the engine-specific backup implementation and
// returns the list of files written, matching commands/service.sh
// cmd_database()'s backup branch. Compression uses Go's built-in gzip
// (compress/gzip) instead of shelling out to a `gzip` binary, so this
// works identically on Windows.
func Backup(p *Profile, opt Options) ([]Result, error) {
	now := time.Now()
	day := now.Format("20060102")
	tstamp := now.Format("150405")
	outDir := filepath.Join(opt.BackupsDir, opt.Domain, "database", opt.Profile, day)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	switch p.Engine {
	case "mysql", "mariadb":
		return backupMySQL(p, opt, outDir, tstamp)
	case "postgresql":
		return backupPostgres(p, opt, outDir, tstamp)
	case "sqlserver":
		return backupSQLServer(p, opt, outDir, tstamp)
	case "sqlite":
		return backupSQLite(p, opt, outDir, tstamp)
	default:
		return nil, fmt.Errorf("unsupported database engine: %s", p.Engine)
	}
}

func databaseList(p *Profile, opt Options, listCmd func() ([]string, error)) ([]string, error) {
	if opt.Database != "" {
		return []string{opt.Database}, nil
	}
	if p.Name != "" {
		return []string{p.Name}, nil
	}
	return listCmd()
}

func gzipToFile(src io.Reader, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	if _, err := io.Copy(gw, src); err != nil {
		gw.Close()
		return err
	}
	return gw.Close()
}

func verifyGzip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	_, err = io.Copy(io.Discard, gr)
	return err
}

func backupMySQL(p *Profile, opt Options, outDir, tstamp string) ([]Result, error) {
	clientBin, hasClient := ClientBin(p.Engine)
	dumpBin, hasDump := DumpBin(p.Engine)
	if !hasDump {
		return nil, fmt.Errorf("database dump tool not found for engine: %s", p.Engine)
	}
	dbs, err := databaseList(p, opt, func() ([]string, error) {
		if !hasClient {
			return nil, fmt.Errorf("database client not found for engine: %s", p.Engine)
		}
		out, err := runWithMySQLPass(clientBin, p.Pass, "-h", p.Host, "-P", orDefault(p.Port, "3306"),
			"-u", orDefault(p.User, "backup"), "-N", "-e", "SHOW DATABASES")
		if err != nil {
			return nil, err
		}
		var names []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			line = strings.TrimSpace(line)
			switch line {
			case "", "information_schema", "mysql", "performance_schema", "sys":
				continue
			}
			names = append(names, line)
		}
		return names, nil
	})
	if err != nil {
		return nil, err
	}

	var results []Result
	for _, db := range dbs {
		outFile := filepath.Join(outDir, fmt.Sprintf("%s_%s.sql.gz", db, tstamp))
		cmd := exec.Command(dumpBin, "-h", p.Host, "-P", orDefault(p.Port, "3306"),
			"-u", orDefault(p.User, "backup"), "--default-character-set="+p.Charset,
			"--single-transaction", "--routines", "--triggers", db)
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+p.Pass)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return results, err
		}
		var stderrBuf strings.Builder
		cmd.Stderr = &stderrBuf
		if err := cmd.Start(); err != nil {
			return results, err
		}
		if err := gzipToFile(stdout, outFile); err != nil {
			cmd.Wait()
			return results, err
		}
		if err := cmd.Wait(); err != nil {
			return results, fmt.Errorf("mysqldump failed for %s: %w: %s", db, err, stderrBuf.String())
		}
		if err := verifyGzip(outFile); err != nil {
			return results, fmt.Errorf("backup verification failed for %s: %w", outFile, err)
		}
		results = append(results, Result{Database: db, Path: outFile})
	}
	return results, nil
}

func runWithMySQLPass(bin, pass string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
	out, err := cmd.Output()
	return string(out), err
}

func backupPostgres(p *Profile, opt Options, outDir, tstamp string) ([]Result, error) {
	clientBin, hasClient := ClientBin(p.Engine)
	dumpBin, hasDump := DumpBin(p.Engine)
	if !hasDump {
		return nil, fmt.Errorf("PostgreSQL dump tool not found: pg_dump")
	}
	dbs, err := databaseList(p, opt, func() ([]string, error) {
		if !hasClient {
			return nil, fmt.Errorf("PostgreSQL client not found: psql")
		}
		cmd := exec.Command(clientBin, "-h", p.Host, "-p", orDefault(p.Port, "5432"),
			"-U", orDefault(p.User, "postgres"), "-d", "postgres", "-At",
			"-c", "SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres');")
		cmd.Env = append(os.Environ(), "PGPASSWORD="+p.Pass)
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		var names []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				names = append(names, line)
			}
		}
		return names, nil
	})
	if err != nil {
		return nil, err
	}

	var results []Result
	for _, db := range dbs {
		outFile := filepath.Join(outDir, fmt.Sprintf("%s_%s.sql.gz", db, tstamp))
		cmd := exec.Command(dumpBin, "-h", p.Host, "-p", orDefault(p.Port, "5432"),
			"-U", orDefault(p.User, "postgres"), "-d", db, "--no-owner", "--no-privileges")
		cmd.Env = append(os.Environ(), "PGPASSWORD="+p.Pass, "PGSSLMODE="+p.SSLMode)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return results, err
		}
		var stderrBuf strings.Builder
		cmd.Stderr = &stderrBuf
		if err := cmd.Start(); err != nil {
			return results, err
		}
		if err := gzipToFile(stdout, outFile); err != nil {
			cmd.Wait()
			return results, err
		}
		if err := cmd.Wait(); err != nil {
			return results, fmt.Errorf("pg_dump failed for %s: %w: %s", db, err, stderrBuf.String())
		}
		if err := verifyGzip(outFile); err != nil {
			return results, fmt.Errorf("backup verification failed for %s: %w", outFile, err)
		}
		results = append(results, Result{Database: db, Path: outFile})
	}
	return results, nil
}

func backupSQLServer(p *Profile, opt Options, outDir, tstamp string) ([]Result, error) {
	_, hasClient := ClientBin(p.Engine)
	dumpBin, hasDump := DumpBin(p.Engine)
	if !hasDump {
		return nil, fmt.Errorf("SQL Server backup requires sqlpackage. Install sqlpackage or set SQLSERVER_DUMP_BIN")
	}
	clientBin, _ := ClientBin(p.Engine)
	dbs, err := databaseList(p, opt, func() ([]string, error) {
		if !hasClient {
			return nil, fmt.Errorf("SQL Server client not found: sqlcmd. Required to list databases when DB_NAME is empty")
		}
		cmd := exec.Command(clientBin, "-S", fmt.Sprintf("%s,%s", p.Host, orDefault(p.Port, "1433")),
			"-U", orDefault(p.User, "sa"), "-P", p.Pass, "-h", "-1", "-W",
			"-Q", "SET NOCOUNT ON; SELECT name FROM sys.databases WHERE database_id > 4;")
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		var names []string
		for _, line := range strings.Split(string(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				names = append(names, line)
			}
		}
		return names, nil
	})
	if err != nil {
		return nil, err
	}

	var results []Result
	for _, db := range dbs {
		outFile := filepath.Join(outDir, fmt.Sprintf("%s_%s.bacpac", db, tstamp))
		cmd := exec.Command(dumpBin,
			"/Action:Export",
			"/SourceServerName:"+fmt.Sprintf("%s,%s", p.Host, orDefault(p.Port, "1433")),
			"/SourceDatabaseName:"+db,
			"/SourceUser:"+orDefault(p.User, "sa"),
			"/SourcePassword:"+p.Pass,
			"/TargetFile:"+outFile,
			"/SourceTrustServerCertificate:"+orDefault(p.TrustServerCertificate, "true"),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return results, fmt.Errorf("sqlpackage export failed for %s: %w: %s", db, err, string(out))
		}
		info, statErr := os.Stat(outFile)
		if statErr != nil || info.Size() == 0 {
			return results, fmt.Errorf("SQL Server export failed: %s", outFile)
		}
		results = append(results, Result{Database: db, Path: outFile})
	}
	return results, nil
}

func backupSQLite(p *Profile, opt Options, outDir, tstamp string) ([]Result, error) {
	clientBin, ok := ClientBin(p.Engine)
	if !ok {
		return nil, fmt.Errorf("SQLite client not found: sqlite3")
	}
	dbPath := p.Path
	if dbPath == "" {
		dbPath = p.Name
	}
	if dbPath == "" {
		return nil, fmt.Errorf("SQLite backup requires DB_PATH or DB_NAME in the profile")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("SQLite database file not found: %s", dbPath)
	}
	base := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	tmpSQLite := filepath.Join(outDir, fmt.Sprintf("%s_%s.sqlite", base, tstamp))
	outFile := tmpSQLite + ".gz"

	cmd := exec.Command(clientBin, dbPath, fmt.Sprintf(".backup '%s'", tmpSQLite))
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sqlite3 backup failed: %w: %s", err, string(out))
	}
	defer os.Remove(tmpSQLite)

	in, err := os.Open(tmpSQLite)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	if err := gzipToFile(in, outFile); err != nil {
		return nil, err
	}
	if err := verifyGzip(outFile); err != nil {
		return nil, fmt.Errorf("backup verification failed for %s: %w", outFile, err)
	}
	return []Result{{Database: base, Path: outFile}}, nil
}

// ApplyRetention deletes backup day-directories older than
// retentionDays, matching the `find ... -mtime +N -exec rm -rf` step in
// cmd_database()'s backup branch.
func ApplyRetention(backupsDir, domain, profile string, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	root := filepath.Join(backupsDir, domain, "database", profile)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
	return nil
}
