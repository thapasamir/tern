//go:build integration

// Package integration tests Tern against real Postgres and Redis, started in containers.
//
// The unit tests use fakes, so they cannot catch a wrong SQL query or a Lua script that behaves
// differently from its Go model. These tests run the real router and a real worker loop against
// the real databases. Run them with `make test-integration` (needs Docker).
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver goose uses
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// The databases are shared by every test. Tests stay independent by creating their own tenant.
var (
	pool *pgxpool.Pool
	rdb  *redis.Client
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tern"), tcpostgres.WithUsername("tern"), tcpostgres.WithPassword("tern"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		log.Printf("starting Postgres (is Docker running?): %v", err)
		return 1
	}
	defer testcontainers.TerminateContainer(pgC) //nolint:errcheck // best effort at exit

	redisC, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		log.Printf("starting Redis: %v", err)
		return 1
	}
	defer testcontainers.TerminateContainer(redisC) //nolint:errcheck // best effort at exit

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("Postgres address: %v", err)
		return 1
	}
	if err := migrate(dsn); err != nil {
		log.Printf("migrating: %v", err)
		return 1
	}
	if pool, err = pgxpool.New(ctx, dsn); err != nil {
		log.Printf("connecting to Postgres: %v", err)
		return 1
	}
	defer pool.Close()

	redisURL, err := redisC.ConnectionString(ctx)
	if err != nil {
		log.Printf("Redis address: %v", err)
		return 1
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Printf("Redis address: %v", err)
		return 1
	}
	rdb = redis.NewClient(opts)
	defer rdb.Close()

	return m.Run()
}

// migrate applies the same migrations production uses.
func migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(db, "../../migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
