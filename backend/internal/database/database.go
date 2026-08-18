package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	Driver  string
	DSN     string
	DataDir string
}

func Open(config Config) (*gorm.DB, error) {
	driver := strings.ToLower(strings.TrimSpace(config.Driver))
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite":
		dsn := strings.TrimSpace(config.DSN)
		if dsn == "" {
			if err := os.MkdirAll(config.DataDir, 0o755); err != nil {
				return nil, err
			}
			// The local Docker setup stores SQLite on a Windows bind mount. WAL
			// sidecar files can become detached from their directory entries on
			// Docker Desktop, leaving the process with stale file handles and
			// causing subsequent requests to fail with "unable to open database
			// file". DELETE journaling keeps the database portable across bind
			// mounts; production deployments use PostgreSQL for concurrency.
			dsn = config.DataDir + "/open_ai_canvas.db?_busy_timeout=5000&_journal_mode=DELETE&_foreign_keys=on&_synchronous=FULL"
		}
		return gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: newParameterizedDatabaseLogger(os.Stdout)})
	case "postgres", "postgresql":
		dsn := strings.TrimSpace(config.DSN)
		if dsn == "" {
			return nil, errors.New("PostgreSQL 模式必须配置 DATABASE_URL")
		}
		return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: newParameterizedDatabaseLogger(os.Stdout)})
	default:
		return nil, fmt.Errorf("不支持的数据库驱动：%s", driver)
	}
}

type parameterizedDatabaseLogger struct {
	logger.Interface
}

func init() {
	// GORM's Scan path uses its package-level recorder before invoking the configured logger.
	// Filter that recorder too, otherwise Scan traces interpolate parameters despite the logger config.
	logger.RecorderParamsFilter = discardDatabaseLogParams
}

func discardDatabaseLogParams(_ context.Context, sql string, _ ...interface{}) (string, []interface{}) {
	return sql, nil
}

func newParameterizedDatabaseLogger(writer io.Writer) logger.Interface {
	base := logger.New(log.New(writer, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	})
	return parameterizedDatabaseLogger{Interface: base}
}

func (databaseLogger parameterizedDatabaseLogger) LogMode(level logger.LogLevel) logger.Interface {
	return parameterizedDatabaseLogger{Interface: databaseLogger.Interface.LogMode(level)}
}

func (databaseLogger parameterizedDatabaseLogger) ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{}) {
	return discardDatabaseLogParams(ctx, sql, params...)
}

func (databaseLogger parameterizedDatabaseLogger) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		err = errors.New("database operation failed")
	}
	databaseLogger.Interface.Trace(ctx, begin, query, err)
}

func ConfigurePool(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		sqlDB.SetMaxOpenConns(30)
		sqlDB.SetMaxIdleConns(10)
		return nil
	}
	// SQLite permits only one writer. A multi-connection pool lets a read
	// transaction race a task/resource write and fail immediately while
	// upgrading to a write transaction, even with busy_timeout configured.
	// Local deployments use SQLite; production concurrency uses PostgreSQL.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	return nil
}
