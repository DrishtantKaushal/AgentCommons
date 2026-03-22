package db

import (
	"database/sql"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection.
type DB struct {
	*sql.DB
	userID    string
	machineID string
}

// CommonsDir returns the path to ~/.commons/
func CommonsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".commons")
}

// DBPath returns the path to the SQLite database file.
func DBPath() string {
	return filepath.Join(CommonsDir(), "commons.db")
}

// Open opens (or creates) the commons SQLite database with WAL mode.
func Open() (*DB, error) {
	dbPath := DBPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create commons dir: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Set WAL mode and pragmas via exec (some pragmas don't work via DSN)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("set %s: %w", pragma, err)
		}
	}

	db := &DB{DB: conn}

	// Run schema migration
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// Ensure default user and machine rows exist
	if err := db.ensureDefaults(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ensure defaults: %w", err)
	}

	return db, nil
}

// OpenInMemory opens an in-memory SQLite database (for testing).
func OpenInMemory() (*DB, error) {
	conn, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}

	db := &DB{DB: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	if err := db.ensureDefaults(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) migrate() error {
	// Strip comment lines, then split on ";" and execute each statement
	var lines []string
	for _, line := range strings.Split(Schema, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		// Skip standalone PRAGMAs (already set via DSN/exec)
		if strings.HasPrefix(trimmed, "PRAGMA") {
			continue
		}
		lines = append(lines, line)
	}
	cleaned := strings.Join(lines, "\n")

	stmts := strings.Split(cleaned, ";")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", truncate(stmt, 80), err)
		}
	}
	return nil
}

func (db *DB) ensureDefaults() error {
	// Get or create user
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		db.userID = uuid.New().String()
		_, err = db.Exec("INSERT INTO users (id, username) VALUES (?, ?)", db.userID, u.Username)
		if err != nil {
			return fmt.Errorf("insert default user: %w", err)
		}
	} else {
		err = db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&db.userID)
		if err != nil {
			return err
		}
	}

	// Get or create machine
	hostname, _ := os.Hostname()
	hardwareID := hostname // simplified for MVP

	err = db.QueryRow("SELECT COUNT(*) FROM machines").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		db.machineID = uuid.New().String()
		_, err = db.Exec(
			"INSERT INTO machines (id, user_id, machine_name, hardware_id) VALUES (?, ?, ?, ?)",
			db.machineID, db.userID, hostname, hardwareID,
		)
		if err != nil {
			return fmt.Errorf("insert default machine: %w", err)
		}
	} else {
		err = db.QueryRow("SELECT id FROM machines LIMIT 1").Scan(&db.machineID)
		if err != nil {
			return err
		}
	}

	return nil
}

// UserID returns the default user ID.
func (db *DB) UserID() string { return db.userID }

// MachineID returns the default machine ID.
func (db *DB) MachineID() string { return db.machineID }

// StartWALCheckpointer runs a periodic WAL checkpoint in the background.
func (db *DB) StartWALCheckpointer(interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
			case <-stop:
				db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
				return
			}
		}
	}()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
