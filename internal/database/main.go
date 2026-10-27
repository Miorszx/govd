package database

import (
	"context"
	"fmt"
	"time"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"github.com/jackc/pgx/v5/pgxpool"
)

var pool *pgxpool.Pool
var queries *Queries

func Init() {
	runMigrations()

	pool = getConnectionPool()

	// convert pool to sqlc queries
	queries = New(pool)
}

func getConnectionPool() *pgxpool.Pool {
	ctx := context.Background()
	dsn := getDSN()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		logger.L.Fatalf("failed to parse database config: %v", err)
	}

	// The bot runs CONCURRENT_UPDATES goroutines and each one may touch the
	// database while handling an update. pgxpool defaults MaxConns to
	// max(4, NumCPU), which on a small VPS leaves most of those goroutines
	// queued behind a handful of connections. Size the pool to the worker
	// count, bounded so the server's max_connections is never exhausted.
	poolSize := int32(config.Env.ConcurrentUpdates) + 4
	if poolSize < 10 {
		poolSize = 10
	}
	if poolSize > 50 {
		poolSize = 50
	}
	cfg.MaxConns = poolSize
	cfg.MinConns = 2
	// Don't let a saturated pool hang a handler forever.
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		logger.L.Fatalf("failed to connect to database: %v", err)
	}
	return pool
}

func getDSN() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		config.Env.DBHost, config.Env.DBPort,
		config.Env.DBName, config.Env.DBUser,
		config.Env.DBPassword,
	)
}

func Q() *Queries {
	return queries
}

func Conn() *pgxpool.Pool {
	return pool
}
