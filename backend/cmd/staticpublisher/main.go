package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"infinite-canvas/backend/internal/service"
)

func main() {
	var directory string
	var release string
	var sourceCommit string
	flag.StringVar(&directory, "dir", "", "Vite 构建产物目录")
	flag.StringVar(&release, "release", "", "不可变发布版本，例如 v1.0.13")
	flag.StringVar(&sourceCommit, "commit", "", "发布标签对应的完整 Git commit SHA")
	flag.Parse()

	config := service.StaticAssetPublishConfig{
		Endpoint:        requiredEnv("HMAIGC_STATIC_OSS_ENDPOINT"),
		Bucket:          requiredEnv("HMAIGC_STATIC_OSS_BUCKET"),
		AccessKeyID:     requiredEnv("HMAIGC_STATIC_OSS_ACCESS_KEY_ID"),
		AccessKeySecret: requiredEnv("HMAIGC_STATIC_OSS_ACCESS_KEY_SECRET"),
		PathPrefix:      requiredEnv("HMAIGC_STATIC_OSS_PREFIX"),
		Release:         release,
		SourceCommit:    sourceCommit,
	}
	if strings.TrimSpace(directory) == "" {
		fatal("必须通过 --dir 指定静态资源构建目录")
	}
	summary, err := service.PublishStaticAssets(directory, config)
	if err != nil {
		fatal(err.Error())
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		fatal(err.Error())
	}
	fmt.Println(string(encoded))
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fatal("缺少环境变量 " + name)
	}
	return value
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "错误："+message)
	os.Exit(1)
}
