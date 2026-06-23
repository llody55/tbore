#!/bin/bash
set -e

# tbore 静态编译脚本
# 用法:
#   ./build.sh             # 编译当前平台
#   ./build.sh amd64       # 编译 linux amd64
#   ./build.sh arm64       # 编译 linux arm64
#   ./build.sh all         # 编译 amd64 和 arm64

TARGETS=()
if [ $# -eq 0 ]; then
    TARGETS+=("$(go env GOARCH)")
else
    for arg in "$@"; do
        case "$arg" in
            amd64|arm64) TARGETS+=("$arg") ;;
            all)          TARGETS=("amd64" "arm64") ;;
            *)
                echo "不支持的架构: $arg"
                echo "用法: $0 [amd64|arm64|all]"
                exit 1
                ;;
        esac
    done
fi

for ARCH in "${TARGETS[@]}"; do
    OUT="tbore-linux-${ARCH}"
    echo "==> 编译 ${OUT} ..."
    CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
        go build -a -ldflags '-extldflags "-static"' \
        -o "${OUT}" tbore.go
    echo "==> 完成: ${OUT} ($(du -h ${OUT} | cut -f1))"
done

echo "全部编译完成"