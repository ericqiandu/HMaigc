#!/usr/bin/env bash

# 生产验活只提取真正启动 SPA 的脚本与样式。完整 CDN 清单已由 Actions 有界验证，部署主机只检查
# HTML 是否精确绑定目标版本 URL；升级事务不得再次联网遍历 CDN，以免网络抖动放大发布耗时。
extract_web_bootstrap_assets() {
    grep -Eo '<script[^>]*>|<link[^>]*>' |
        awk '
            /^<script/ && /src="[^"]+\.js(\?[^"]*)?"/ {
                value = $0
                sub(/^.*src="/, "", value)
                sub(/".*$/, "", value)
                print value
            }
            /^<link/ && /rel="stylesheet"/ && /href="[^"]+\.css(\?[^"]*)?"/ {
                value = $0
                sub(/^.*href="/, "", value)
                sub(/".*$/, "", value)
                print value
            }
        ' |
        awk '!seen[$0]++'
}
