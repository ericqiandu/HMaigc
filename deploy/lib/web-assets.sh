#!/usr/bin/env bash

# 生产验活只探测真正启动 SPA 的同源脚本与样式。其余哈希构建资产已封装在同一个不可变 Web 镜像中，
# 若在每次升级时再次串行遍历，会无意义地放大部署耗时。
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
