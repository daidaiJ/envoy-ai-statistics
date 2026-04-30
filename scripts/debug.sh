#!/bin/sh
# 日志等级控制脚本
# 用法: ./debug.sh on|off|status

HTTP_ADDR="${HTTP_ADDR:-localhost:8889}"

case "$1" in
    on)
        echo "开启 debug 模式..."
        curl -s -X PUT "http://${HTTP_ADDR}/log/level" -d '{"level":"debug"}'
        ;;
    off)
        echo "关闭 debug 模式..."
        curl -s -X PUT "http://${HTTP_ADDR}/log/level" -d '{"level":"info"}'
        ;;
    status)
        curl -s "http://${HTTP_ADDR}/log/level"
        ;;
    *)
        echo "用法: $0 on|off|status"
        echo "  on     - 开启 debug 模式"
        echo "  off    - 关闭 debug 模式（恢复 info）"
        echo "  status - 查看当前日志等级"
        exit 1
        ;;
esac