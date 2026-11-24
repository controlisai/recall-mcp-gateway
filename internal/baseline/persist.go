package baseline

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/store"
	_ "modernc.org/sqlite"
)

// PersistedState holds all state that survives proxy restarts.
type PersistedState struct {
	Baselines    map[string]*Baseline    `json:"baselines"`
	ServerHealth map[string]ServerHealth `json:"serverHealth,omitempty"`
	LastUpdated  time.Time               `json:"lastUpdated"`
}

// DefaultStatePath returns the default path for persisted state.
func DefaultStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".recall-mcp-gateway", "baselines.json")
}

// SaveState persists the tracker state to a JSON file.
func SaveState(path string, tracker *Tracker, serverHealth map[string]ServerHealth) error {
	state := PersistedState{
		Baselines:    tracker.All(),
		ServerHealth: serverHealth,
		LastUpdated:  time.Now(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	return nil
}

// LoadState reads persisted state from a JSON file.
// Returns nil state (not an error) if the file doesn't exist.
func LoadState(path string) (*PersistedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no state file = fresh start
		}
		return nil, fmt.Errorf("read state: %w", err)
	}

	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}

	age := time.Since(state.LastUpdated)
	slog.Info("baseline state loaded", "component", "baseline", "age", age.Round(time.Minute), "baseline_count", len(state.Baselines))

	return &state, nil
}

// SaveStateToDB persists tracker state and server health into the shared gateway DB.
// Each key is stored as a JSON blob in the baselines table.
// Uses UPSERT (not DELETE+INSERT) so multi-pod deployments don't overwrite each other.
func SaveStateToDB(db *sql.DB, tracker *Tracker, serverHealth map[string]ServerHealth) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	isPG := store.IsPostgres(db)
	now := time.Now().UTC().Format(time.RFC3339)
	for key, bl := range tracker.All() {
		data, err := json.Marshal(bl)
		if err != nil {
			continue
		}
		if _, err := store.ExecDialectTx(tx,
			"INSERT OR REPLACE INTO baselines (key, data, updated_at) VALUES (?, ?, ?)",
			"INSERT INTO baselines (key, data, updated_at) VALUES ($1, $2, $3) ON CONFLICT (key) DO UPDATE SET data = $2, updated_at = $3",
			isPG,
			"bl:"+key, string(data), now,
		); err != nil {
			return fmt.Errorf("insert baseline %s: %w", key, err)
		}
	}
	for name, sh := range serverHealth {
		data, err := json.Marshal(sh)
		if err != nil {
			continue
		}
		if _, err := store.ExecDialectTx(tx,
			"INSERT OR REPLACE INTO baselines (key, data, updated_at) VALUES (?, ?, ?)",
			"INSERT INTO baselines (key, data, updated_at) VALUES ($1, $2, $3) ON CONFLICT (key) DO UPDATE SET data = $2, updated_at = $3",
			isPG,
			"sh:"+name, string(data), now,
		); err != nil {
			return fmt.Errorf("insert server health %s: %w", name, err)
		}
	}

	return tx.Commit()
}

// LoadStateFromDB reads persisted baseline state from the shared gateway DB.
// Returns a *PersistedState (nil if no rows found — fresh start).
func LoadStateFromDB(db *sql.DB) (*PersistedState, error) {
	rows, err := db.Query("SELECT key, data FROM baselines")
	if err != nil {
		return nil, fmt.Errorf("query baselines: %w", err)
	}
	defer rows.Close()

	state := &PersistedState{
		Baselines:    make(map[string]*Baseline),
		ServerHealth: make(map[string]ServerHealth),
		LastUpdated:  time.Now(),
	}
	count := 0
	for rows.Next() {
		var key, data string
		if rows.Scan(&key, &data) != nil {
			continue
		}
		if len(key) > 3 && key[:3] == "bl:" {
			var bl Baseline
			if json.Unmarshal([]byte(data), &bl) == nil {
				state.Baselines[key[3:]] = &bl
				count++
			}
		} else if len(key) > 3 && key[:3] == "sh:" {
			var sh ServerHealth
			if json.Unmarshal([]byte(data), &sh) == nil {
				state.ServerHealth[key[3:]] = sh
			}
		}
	}
	if count == 0 && len(state.ServerHealth) == 0 {
		return nil, nil // no state = fresh start
	}
	slog.Info("baseline state loaded", "component", "baseline", "baseline_count", count, "storage", "gateway.db")
	return state, nil
}

// StartPeriodicSaveToDB starts a goroutine that saves state to the DB every interval.
// On stop signal, performs a final save before exiting.
func StartPeriodicSaveToDB(db *sql.DB, tracker *Tracker, latency *LatencyTracker, interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := SaveStateToDB(db, tracker, latency.All()); err != nil {
					slog.Warn("failed to save baselines to gateway.db", "component", "baseline", "error", err)
				}
			case <-stop:
				SaveStateToDB(db, tracker, latency.All()) //nolint:errcheck — best-effort final save
				return
			}
		}
	}()
}
