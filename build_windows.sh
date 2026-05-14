#!/bin/bash

set -e

ENV_FILE="/Users/cat/Source/water-reminder/.env.bankgpt"
BACKUP_FILE="main.go.bak"
OUTPUT_DIR="build/windows"
OUTPUT_NAME="water-reminder.exe"

echo "=== Windows 构建脚本 ==="
echo ""

if [ ! -f "$ENV_FILE" ]; then
    echo "❌ 错误: 环境文件不存在: $ENV_FILE"
    exit 1
fi

echo "📁 使用环境文件: $ENV_FILE"
echo ""

echo "📝 读取配置..."
API_KEY=$(grep "^WATER_REMINDER_API_KEY=" "$ENV_FILE" | cut -d'=' -f2)
BASE_URL=$(grep "^WATER_REMINDER_BASE_URL=" "$ENV_FILE" | cut -d'=' -f2)
MODEL_NAME=$(grep "^WATER_REMINDER_MODEL_NAME=" "$ENV_FILE" | cut -d'=' -f2)

echo "  API_KEY: ${API_KEY:0:10}..."
echo "  BASE_URL: $BASE_URL"
echo "  MODEL_NAME: $MODEL_NAME"
echo ""

echo "🔄 备份原始文件..."
cp main.go "$BACKUP_FILE"

echo "🔧 替换占位符..."
sed -i "" "s/__API_KEY_PLACEHOLDER__/$API_KEY/g" main.go
sed -i "" "s#__BASE_URL_PLACEHOLDER__#$BASE_URL#g" main.go
sed -i "" "s/__MODEL_NAME_PLACEHOLDER__/$MODEL_NAME/g" main.go

echo "📦 创建输出目录..."
mkdir -p "$OUTPUT_DIR"

echo "🔨 构建 Windows 版本..."
GOOS=windows GOARCH=amd64 go build -o "$OUTPUT_DIR/$OUTPUT_NAME" .

echo "🔄 恢复原始文件..."
mv "$BACKUP_FILE" main.go

echo ""
echo "✅ 构建完成!"
echo ""
echo "📁 输出文件: $OUTPUT_DIR/$OUTPUT_NAME"
echo ""
echo "📋 使用方法:"
echo "  1. 将 $OUTPUT_DIR/$OUTPUT_NAME 复制到 Windows 机器"
echo "  2. 双击运行或在命令行执行"
echo ""
echo "⚙️ 配置优先级:"
echo "  1. 运行时环境变量"
echo "  2. 应用同级目录的 .env 文件"
echo "  3. 编译时注入的默认值"
echo ""