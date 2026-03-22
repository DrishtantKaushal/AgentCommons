package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/DrishtantKaushal/AgentCommons/internal/config"
	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"

	"encoding/json"
)

// Run starts the daemon process. This is the main entry point for `commons server start`.
func Run(cfg config.Config) error {
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	srv := NewServer(database)

	// Start WAL checkpoint scheduler
	stop := make(chan struct{})
	database.StartWALCheckpointer(cfg.WALCheckpoint, stop)

	// Start heartbeat reaper
	srv.StartReaper(cfg.ReaperInterval, cfg.HeartbeatTimeout, cfg.GracePeriod, stop)

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		log.Printf("[daemon] received %s, shutting down gracefully", sig)

		// Broadcast shutdown to all clients
		payload, _ := json.Marshal(protocol.ServerShuttingDown{
			Reason:        "manual_stop",
			GracePeriodMs: 5000,
		})
		srv.broadcast(&protocol.Envelope{
			Type:    protocol.TypeServerShuttingDown,
			Payload: payload,
		}, "")

		close(stop)

		// Close all client connections
		srv.clientsMu.Lock()
		for _, c := range srv.clients {
			c.Conn.Close()
		}
		srv.clientsMu.Unlock()

		// Close database (flushes WAL)
		database.Close()

		os.Exit(0)
	}()

	addr := Addr(cfg.Port)
	log.Printf("[daemon] starting on %s", addr)

	// Write PID file
	pidPath := db.CommonsDir() + "/daemon.pid"
	os.MkdirAll(db.CommonsDir(), 0755)
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		log.Printf("[daemon] warning: could not write PID file: %v", err)
	}

	return srv.ListenAndServe(addr)
}
