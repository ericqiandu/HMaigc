package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type tableMigration struct {
	name string
	run  func(source *gorm.DB, target *gorm.DB, copyRows bool) (int, error)
}

func main() {
	sourcePath := strings.TrimSpace(os.Getenv("SQLITE_SOURCE_PATH"))
	targetDSN := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if sourcePath == "" || targetDSN == "" {
		log.Fatal("必须配置 SQLITE_SOURCE_PATH 和 DATABASE_URL")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		log.Fatalf("读取 SQLite 源文件失败：%v", err)
	}

	source, err := database.Open(database.Config{Driver: "sqlite", DSN: "file:" + sourcePath + "?mode=ro&_busy_timeout=5000"})
	if err != nil {
		log.Fatalf("连接 SQLite 失败：%v", err)
	}
	target, err := database.Open(database.Config{Driver: "postgres", DSN: targetDSN})
	if err != nil {
		log.Fatalf("连接 PostgreSQL 失败：%v", err)
	}
	if err := verifySQLite(source); err != nil {
		log.Fatalf("SQLite 完整性检查失败：%v", err)
	}
	if err := verifyMigrationCoverage(source); err != nil {
		log.Fatalf("迁移表清单检查失败：%v", err)
	}
	source = source.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	target = target.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})

	// PostgreSQL 的 DDL 参与事务；任一表复制或核对失败都会回滚整个新库结构。
	if err := target.Transaction(func(tx *gorm.DB) error {
		tableCount, err := publicTableCount(tx)
		if err != nil {
			return err
		}
		copyRows := tableCount == 0
		if !copyRows && tableCount != int64(len(migrations())) {
			return fmt.Errorf("PostgreSQL public schema 已有 %d 张表，拒绝覆盖或补写", tableCount)
		}
		return migrateApplicationTables(source, tx, copyRows)
	}); err != nil {
		log.Fatal(err)
	}
}

func migrateApplicationTables(source *gorm.DB, target *gorm.DB, copyRows bool) error {
	if err := database.MigrateBaseSchema(target); err != nil {
		return fmt.Errorf("创建目标基础表结构：%w", err)
	}
	total := 0
	for _, migration := range migrations() {
		count, err := migration.run(source, target, copyRows)
		if err != nil {
			return fmt.Errorf("迁移表 %s：%w", migration.name, err)
		}
		total += count
		log.Printf("已迁移并核对 %s：%d 行", migration.name, count)
	}
	if err := database.EnsurePaymentIntegritySchema(target); err != nil {
		return fmt.Errorf("施加支付完整性约束：%w", err)
	}
	if err := database.EnsureProviderIntegritySchema(target); err != nil {
		return fmt.Errorf("施加平台事实完整性约束：%w", err)
	}
	if copyRows {
		log.Printf("全量迁移核对完成：%d 张表，%d 行", len(migrations()), total)
	} else {
		log.Printf("目标库已有完整迁移结果，未重复写入：%d 张表，%d 行", len(migrations()), total)
	}
	return nil
}

func verifySQLite(db *gorm.DB) error {
	var result string
	if err := db.Raw("PRAGMA quick_check").Scan(&result).Error; err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("quick_check 返回 %q", result)
	}
	return nil
}

func publicTableCount(db *gorm.DB) (int64, error) {
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&count).Error
	return count, err
}

func migrateModel(name string, value any) tableMigration {
	return tableMigration{
		name: name,
		run: func(source *gorm.DB, target *gorm.DB, copyRows bool) (int, error) {
			primaryKey, err := primaryKeyColumn(source, value)
			if err != nil {
				return 0, err
			}
			modelType := reflect.TypeOf(value)
			if modelType.Kind() == reflect.Pointer {
				modelType = modelType.Elem()
			}
			sourceRows := reflect.New(reflect.SliceOf(modelType))
			if err := source.Order(primaryKey).Find(sourceRows.Interface()).Error; err != nil {
				return 0, err
			}
			if copyRows && sourceRows.Elem().Len() > 0 {
				if err := target.CreateInBatches(sourceRows.Interface(), 100).Error; err != nil {
					return 0, err
				}
			}

			targetRows := reflect.New(reflect.SliceOf(modelType))
			if err := target.Order(primaryKey).Find(targetRows.Interface()).Error; err != nil {
				return 0, err
			}
			if !equivalent(sourceRows.Elem(), targetRows.Elem()) {
				return 0, errors.New("源数据与目标数据逐字段核对不一致")
			}
			return sourceRows.Elem().Len(), nil
		},
	}
}

func migratePaymentWebhookEvents() tableMigration {
	return tableMigration{
		name: "payment_webhook_events",
		run: func(source *gorm.DB, target *gorm.DB, copyRows bool) (int, error) {
			var sourceRows []model.PaymentWebhookEvent
			if err := source.Order("id").Find(&sourceRows).Error; err != nil {
				return 0, err
			}
			for index := range sourceRows {
				if err := normalizeProcessedWebhookFacts(source, &sourceRows[index]); err != nil {
					return 0, err
				}
			}
			if copyRows && len(sourceRows) > 0 {
				if err := target.CreateInBatches(&sourceRows, 100).Error; err != nil {
					return 0, err
				}
			}

			var targetRows []model.PaymentWebhookEvent
			if err := target.Order("id").Find(&targetRows).Error; err != nil {
				return 0, err
			}
			if !equivalent(reflect.ValueOf(sourceRows), reflect.ValueOf(targetRows)) {
				return 0, errors.New("源数据与目标数据逐字段核对不一致")
			}
			return len(sourceRows), nil
		},
	}
}

func normalizeProcessedWebhookFacts(source *gorm.DB, event *model.PaymentWebhookEvent) error {
	if event.Status != model.PaymentWebhookProcessed {
		return nil
	}
	if strings.TrimSpace(event.TransactionID) == "" {
		return fmt.Errorf("已处理支付回调 %s 缺少 transaction_id，无法迁移商户事实", event.ID)
	}
	var transaction model.PaymentTransaction
	if err := source.First(&transaction, "id = ?", event.TransactionID).Error; err != nil {
		return fmt.Errorf("已处理支付回调 %s 找不到交易 %s，无法迁移商户事实: %w", event.ID, event.TransactionID, err)
	}
	normalized, _, err := database.NormalizeProcessedPaymentWebhookFacts(*event, transaction)
	if err != nil {
		return err
	}
	*event = normalized
	return nil
}

func primaryKeyColumn(db *gorm.DB, value any) (string, error) {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(value); err != nil {
		return "", err
	}
	if len(statement.Schema.PrimaryFields) != 1 {
		return "", fmt.Errorf("表 %s 必须有且只有一个主键", statement.Schema.Table)
	}
	return statement.Schema.PrimaryFields[0].DBName, nil
}

var timeType = reflect.TypeOf(time.Time{})

func equivalent(left reflect.Value, right reflect.Value) bool {
	if !left.IsValid() || !right.IsValid() {
		return left.IsValid() == right.IsValid()
	}
	if left.Type() != right.Type() {
		return false
	}
	if left.Type() == timeType {
		leftTime := left.Interface().(time.Time).Truncate(time.Microsecond)
		rightTime := right.Interface().(time.Time).Truncate(time.Microsecond)
		return leftTime.Equal(rightTime)
	}
	switch left.Kind() {
	case reflect.Pointer, reflect.Interface:
		if left.IsNil() || right.IsNil() {
			return left.IsNil() == right.IsNil()
		}
		return equivalent(left.Elem(), right.Elem())
	case reflect.Slice, reflect.Array:
		if left.Len() != right.Len() {
			return false
		}
		for index := 0; index < left.Len(); index++ {
			if !equivalent(left.Index(index), right.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for index := 0; index < left.NumField(); index++ {
			field := left.Type().Field(index)
			if field.Tag.Get("gorm") == "-" || strings.Contains(field.Tag.Get("gorm"), "->") {
				continue
			}
			if !equivalent(left.Field(index), right.Field(index)) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(left.Interface(), right.Interface())
	}
}

func verifyMigrationCoverage(db *gorm.DB) error {
	expected := make(map[string]struct{}, len(database.Models()))
	for _, migration := range migrations() {
		if _, exists := expected[migration.name]; exists {
			return fmt.Errorf("应用模型清单包含重复表 %s", migration.name)
		}
		expected[migration.name] = struct{}{}
	}
	if len(expected) != len(database.Models()) {
		return fmt.Errorf("应用模型表数量=%d，迁移表数量=%d", len(database.Models()), len(expected))
	}
	return nil
}

func migrations() []tableMigration {
	models := database.Models()
	result := make([]tableMigration, 0, len(models))
	cache := &sync.Map{}
	for _, value := range models {
		parsed, err := schema.Parse(value, cache, schema.NamingStrategy{})
		if err != nil {
			panic(fmt.Sprintf("解析应用模型 %T 失败: %v", value, err))
		}
		if _, ok := value.(*model.PaymentWebhookEvent); ok {
			result = append(result, migratePaymentWebhookEvents())
			continue
		}
		result = append(result, migrateModel(parsed.Table, value))
	}
	return result
}
