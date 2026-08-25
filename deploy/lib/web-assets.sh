#!/usr/bin/env bash

# 生产验活只探测真正启动 SPA 的脚本与样式。modulepreload 等构建资产已在发布器中逐文件校验，
# 若在每次升级时再次串行下载，会把单个 CDN 抖动放大成数十分钟的运维任务。
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
