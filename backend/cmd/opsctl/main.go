package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("用法：hmaigc-opsctl status | upgrade vX.Y.Z | rollback | backup | verify | cancel <id> | recover <id>")
	}
	stateDir := env("HMAIGC_OPS_STATE_DIR", "/var/lib/hmaigc-ops")
	secret, err := os.ReadFile(env("HMAIGC_OPS_SHARED_SECRET_FILE", filepath.Join(stateDir, "shared-secret")))
	if err != nil {
		return err
	}
	client, err := opsprotocol.NewUnixClient(env("HMAIGC_OPS_SOCKET", filepath.Join(stateDir, "controller.sock")), []byte(strings.TrimSpace(string(secret))))
	if err != nil {
		return err
	}
	ctx := context.Background()
	command := strings.ToLower(strings.TrimSpace(os.Args[1]))
	if command == "status" {
		overview, err := client.Overview(ctx)
		if err != nil {
			return err
		}
		return printJSON(overview)
	}
	if command == "cancel" || command == "recover" {
		if len(os.Args) != 3 {
			return errors.New(command + " 必须指定运维任务 ID")
		}
		operationID := strings.TrimSpace(os.Args[2])
		idempotencyKey := newIdempotencyKey()
		var operation *opsprotocol.Operation
		if command == "cancel" {
			operation, err = client.CancelOperation(ctx, operationID, opsprotocol.CancelOperationRequest{
				ActorUserID: "system-bootstrap", ActorDisplayName: "命令行运维",
				IdempotencyKey: idempotencyKey, Confirmation: "STOP " + operationID,
			})
		} else {
			operation, err = client.RecoverOperation(ctx, operationID, opsprotocol.RecoverOperationRequest{
				ActorUserID: "system-bootstrap", ActorDisplayName: "命令行运维",
				IdempotencyKey: idempotencyKey, Confirmation: "RECOVER " + operationID,
			})
		}
		if err != nil {
			return err
		}
		fmt.Printf("运维控制请求已接受：%s\n", operation.ID)
		return waitOperation(ctx, client, operation.ID)
	}
	action := opsprotocol.Action(command)
	target := ""
	if action == opsprotocol.ActionUpgrade {
		if len(os.Args) != 3 {
			return errors.New(command + " 必须指定 vX.Y.Z 目标版本")
		}
		target = strings.TrimSpace(os.Args[2])
	}
	confirmation, err := confirmationFor(action, target)
	if err != nil {
		return err
	}
	operation, err := client.StartOperation(ctx, opsprotocol.StartOperationRequest{
		Action: action, TargetVersion: target, ActorUserID: "system-bootstrap",
		ActorDisplayName: "命令行运维", IdempotencyKey: newIdempotencyKey(),
		Confirmation: confirmation,
	})
	if err != nil {
		return err
	}
	fmt.Printf("运维任务已创建：%s\n", operation.ID)
	return waitOperation(ctx, client, operation.ID)
}

func waitOperation(ctx context.Context, client opsprotocol.Client, id string) error {
	var cursor uint64
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		logs, err := client.OperationLogs(ctx, id, cursor, 500)
		if err != nil {
			return err
		}
		for _, entry := range logs.Items {
			fmt.Printf("%s [%s] %s\n", entry.CreatedAt.Format(time.RFC3339), entry.Stream, entry.Message)
		}
		cursor = logs.NextCursor
		operation, err := client.Operation(ctx, id)
		if err != nil {
			return err
		}
		switch operation.Status {
		case opsprotocol.OperationSucceeded:
			return nil
		case opsprotocol.OperationFailed:
			return errors.New(operation.Error)
		case opsprotocol.OperationCancelled:
			return nil
		case opsprotocol.OperationRecoveryRequired:
			return errors.New("运维任务需要人工恢复: " + operation.Error)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func confirmationFor(action opsprotocol.Action, target string) (string, error) {
	switch action {
	case opsprotocol.ActionUpgrade:
		return "UPGRADE " + target, nil
	case opsprotocol.ActionRollback:
		return "ROLLBACK", nil
	case opsprotocol.ActionBackup:
		return "BACKUP", nil
	case opsprotocol.ActionVerify:
		return "VERIFY", nil
	default:
		return "", errors.New("不支持的运维命令")
	}
}

func newIdempotencyKey() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("opsctl-%d", time.Now().UnixNano())
	}
	return "opsctl-" + hex.EncodeToString(raw[:])
}

func printJSON(value interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
