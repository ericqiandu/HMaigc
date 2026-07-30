package buildinfo

// Version 与 Commit 由生产镜像在编译时通过 -ldflags 注入。
// 本地直接 go run 时保留显式的开发标识，部署脚本不会把开发构建误判为生产版本。
var (
	Version = "dev"
	Commit  = "unknown"
)
