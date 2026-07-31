package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/opscontroller"
)

func main() {
	stateDir := env("HMAIGC_OPS_STATE_DIR", "/var/lib/hmaigc-ops")
	backendGID, err := strconv.Atoi(env("HMAIGC_BACKEND_GID", "101"))
	if err != nil || backendGID < 1 {
		log.Fatal("HMAIGC_BACKEND_GID 必须是正整数")
	}
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		log.Fatal(err)
	}
	if err := os.Chown(stateDir, 0, backendGID); err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0o750); err != nil {
		log.Fatal(err)
	}
	secretPath := env("HMAIGC_OPS_SHARED_SECRET_FILE", filepath.Join(stateDir, "shared-secret"))
	secret, err := loadOrCreateSecret(secretPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chown(secretPath, 0, backendGID); err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod(secretPath, 0o640); err != nil {
		log.Fatal(err)
	}
	store, err := opscontroller.OpenStore(filepath.Join(stateDir, "controller.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close controller store: %v", err)
		}
	}()
	scriptPath := env("HMAIGC_DEPLOY_SCRIPT", "/opt/hmaigc/deploy/hmaigc.sh")
	envFile := env("HMAIGC_ENV_FILE", "/opt/hmaigc/.env.production")
	if err := validateReadableFile(scriptPath); err != nil {
		log.Fatal(err)
	}
	if err := validateReadableFile(envFile); err != nil {
		log.Fatal(err)
	}
	controller, err := opscontroller.New(
		store,
		opscontroller.ScriptExecutor{ScriptPath: scriptPath, EnvFile: envFile},
		opscontroller.GitHubReleaseSource{
			URL:   env("HMAIGC_RELEASES_API_URL", ""),
			Token: strings.TrimSpace(os.Getenv("HMAIGC_RELEASES_API_TOKEN")),
		},
		opscontroller.Config{
			StateFile: env("HMAIGC_RELEASE_STATE_FILE", filepath.Join(stateDir, "release", "release.env")),
			BackupDir: env("HMAIGC_BACKUP_DIR", filepath.Join(stateDir, "backups")),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	handler, err := opscontroller.NewHTTPHandler(controller, secret)
	if err != nil {
		log.Fatal(err)
	}
	socketPath := env("HMAIGC_OPS_SOCKET", filepath.Join(stateDir, "controller.sock"))
	listener, err := listenUnix(socketPath)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	mode, err := parseSocketMode(env("HMAIGC_OPS_SOCKET_MODE", "0660"))
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod(socketPath, mode); err != nil {
		log.Fatal(err)
	}
	if err := os.Chown(socketPath, 0, backendGID); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove controller socket: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go controller.Run(ctx)
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	result := make(chan error, 1)
	go func() {
		log.Printf("HMaigc ops controller listening on unix://%s", socketPath)
		result <- server.Serve(listener)
	}()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			log.Printf("ops controller shutdown: %v", err)
		}
	}
}

func listenUnix(path string) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket 路径已存在且不是 Unix socket: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return net.Listen("unix", path)
}

func loadOrCreateSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		secret := []byte(strings.TrimSpace(string(data)))
		if len(secret) < 32 {
			return nil, errors.New("运维控制器共享密钥长度不足")
		}
		return secret, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	secret := []byte(hex.EncodeToString(raw[:]))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(append(secret, '\n')); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return secret, nil
}

func parseSocketMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, errors.New("HMAIGC_OPS_SOCKET_MODE 必须是八进制权限")
	}
	mode := os.FileMode(parsed)
	switch mode {
	case 0o600, 0o660, 0o666:
		return mode, nil
	default:
		return 0, errors.New("HMAIGC_OPS_SOCKET_MODE 仅允许 0600、0660 或 0666")
	}
}

func validateReadableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("不是普通文件: %s", path)
	}
	return nil
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
