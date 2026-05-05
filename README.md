# 喝了么 - 喝水提醒工具

一个有趣的喝水提醒工具，使用大模型生成幽默有趣的提醒文案。

## 功能特性

- 🍵 定时提醒喝水（可配置提醒间隔）
- 🤖 使用大模型生成有趣的提醒文案
- 🔔 系统通知推送（支持 macOS、Linux、Windows）
- 📋 菜单栏常驻图标
- ⚡ 支持通过环境变量配置参数
- 🔄 大模型请求异常重试机制（指数退避）

## 安装

### 直接下载

暂无预编译版本，请自行编译。

### 从源码编译

```bash
# 克隆项目
git clone <repository-url>
cd water-reminder

# （可选）编译前替换占位符设置默认配置
# 替换 API 密钥
sed -i.bak 's/__API_KEY_PLACEHOLDER__/your-api-key/g' main.go
# 替换 API 地址
sed -i.bak 's/__BASE_URL_PLACEHOLDER__/https://your-api-url/g' main.go
# 替换模型名称
sed -i.bak 's/__MODEL_NAME_PLACEHOLDER__/qwen3:32b/g' main.go

# 编译
go build -o water-reminder .
```

## 使用

### 基本使用

```bash
# 启动应用
./water-reminder

# 显示帮助
./water-reminder -help
```

### 配置方式

可以通过环境变量配置以下参数：

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| `WATER_REMINDER_API_KEY` | 大模型 API 密钥 | 编译时注入 |
| `WATER_REMINDER_BASE_URL` | API 接口地址 | 编译时注入 |
| `WATER_REMINDER_MODEL_NAME` | 模型名称 | 编译时注入 |
| `WATER_REMINDER_REMIND_GAP` | 提醒间隔 | 35m |
| `WATER_REMINDER_MAX_RETRIES` | 最大重试次数 | 3 |
| `WATER_REMINDER_INITIAL_DELAY` | 初始重试间隔 | 1s |
| `WATER_REMINDER_MAX_DELAY` | 最大重试间隔 | 5s |
| `WATER_REMINDER_TIMEOUT` | 请求超时时间 | 120s |

**配置优先级**（从高到低）：
1. 运行时环境变量
2. 当前目录下的 `.env` 文件（运行时读取）
3. 编译时通过占位符注入的值
4. 代码默认值

**使用 .env 文件**

`.env` 文件有两个用途：

**用途一：运行时配置**

在应用程序同级目录创建 `.env` 文件，运行时自动读取：

```env
WATER_REMINDER_API_KEY=your-api-key
WATER_REMINDER_BASE_URL=https://your-api-url
WATER_REMINDER_MODEL_NAME=qwen3:32b
WATER_REMINDER_REMIND_GAP=35m
```

**用途二：构建时配置（生成默认值）**

可以在编译前使用 `.env` 文件的内容自动替换代码中的占位符：

```bash
# 从 .env 文件读取配置并替换占位符
ENV_FILE=.env
if [ -f "$ENV_FILE" ]; then
  while IFS='=' read -r key value; do
    case "$key" in
      WATER_REMINDER_API_KEY)
        sed -i.bak "s/__API_KEY_PLACEHOLDER__/$value/g" main.go
        ;;
      WATER_REMINDER_BASE_URL)
        sed -i.bak "s/__BASE_URL_PLACEHOLDER__/$value/g" main.go
        ;;
      WATER_REMINDER_MODEL_NAME)
        sed -i.bak "s/__MODEL_NAME_PLACEHOLDER__/$value/g" main.go
        ;;
    esac
  done < "$ENV_FILE"
fi

# 编译
go build -o water-reminder .
```

### 使用示例

```bash
# 设置 API 密钥后启动
WATER_REMINDER_API_KEY=sk-xxx ./water-reminder

# 设置提醒间隔为5分钟
export WATER_REMINDER_REMIND_GAP=5m
./water-reminder

# 设置使用的模型
export WATER_REMINDER_MODEL_NAME=deepseek-chat
./water-reminder
```

## 交叉编译

### 编译 Windows 版本

```bash
# 设置环境变量
export GOOS=windows
export GOARCH=amd64

# 编译
go build -o water-reminder.exe .
```

### 编译 Linux 版本

```bash
# 设置环境变量
export GOOS=linux
export GOARCH=amd64

# 编译
go build -o water-reminder-linux .
```

### 编译 macOS 版本

```bash
# Intel Mac
export GOOS=darwin
export GOARCH=amd64
go build -o water-reminder-darwin-amd64 .

# Apple Silicon Mac
export GOOS=darwin
export GOARCH=arm64
go build -o water-reminder-darwin-arm64 .
```

### 一键编译多个平台（使用 Makefile）

创建 `Makefile`：

```makefile
.PHONY: all windows linux mac

all: windows linux mac

windows:
	GOOS=windows GOARCH=amd64 go build -o build/windows/water-reminder.exe .

linux:
	GOOS=linux GOARCH=amd64 go build -o build/linux/water-reminder .

mac:
	GOOS=darwin GOARCH=amd64 go build -o build/mac/water-reminder-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o build/mac/water-reminder-arm64 .

clean:
	rm -rf build/
```

使用方式：

```bash
# 编译所有平台
make all

# 只编译 Windows
make windows

# 清理
make clean
```

## macOS 应用包

### 创建 macOS .app 包

```bash
# 创建目录结构
mkdir -p build/macos/water-reminder.app/Contents/{MacOS,Resources}

# 编译二进制
GOOS=darwin GOARCH=arm64 go build -o build/macos/water-reminder.app/Contents/MacOS/water-reminder .

# 创建 Info.plist
cat > build/macos/water-reminder.app/Contents/Info.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>water-reminder</string>
	<key>CFBundleDisplayName</key>
	<string>赛博水友</string>
	<key>CFBundleIdentifier</key>
	<string>com.example.water-reminder</string>
	<key>CFBundleVersion</key>
	<string>1.0.0</string>
	<key>CFBundleShortVersionString</key>
	<string>1.0</string>
	<key>CFBundleExecutable</key>
	<string>water-reminder</string>
	<key>CFBundleIconFile</key>
	<string>water-reminder.icns</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
EOF

# 转换图标为 icns 格式（需要安装 Xcode 命令行工具）
mkdir -p /tmp/icon.iconset
sips -z 16 16 water_reminder_logo2.png --out /tmp/icon.iconset/icon_16x16.png
sips -z 32 32 water_reminder_logo2.png --out /tmp/icon.iconset/icon_16x16@2x.png
sips -z 32 32 water_reminder_logo2.png --out /tmp/icon.iconset/icon_32x32.png
sips -z 64 64 water_reminder_logo2.png --out /tmp/icon.iconset/icon_32x32@2x.png
sips -z 128 128 water_reminder_logo2.png --out /tmp/icon.iconset/icon_128x128.png
sips -z 256 256 water_reminder_logo2.png --out /tmp/icon.iconset/icon_128x128@2x.png
sips -z 256 256 water_reminder_logo2.png --out /tmp/icon.iconset/icon_256x256.png
sips -z 512 512 water_reminder_logo2.png --out /tmp/icon.iconset/icon_256x256@2x.png
sips -z 512 512 water_reminder_logo2.png --out /tmp/icon.iconset/icon_512x512.png
sips -z 1024 1024 water_reminder_logo2.png --out /tmp/icon.iconset/icon_512x512@2x.png
iconutil -c icns /tmp/icon.iconset -o build/macos/water-reminder.app/Contents/Resources/water-reminder.icns
```

### 使用应用包

```bash
# 运行应用
open build/macos/water-reminder.app

# 或复制到 Applications 目录
cp -r build/macos/water-reminder.app /Applications/
```

## 依赖

- Go 1.16+
- 以下第三方库（已包含在 vendor 目录）：
  - `github.com/gen2brain/beeep` - 系统通知
  - `github.com/getlantern/systray` - 系统托盘图标
  - `github.com/sashabaranov/go-openai` - OpenAI API 客户端

## 注意事项

1. **macOS 通知图标**：需要安装 `terminal-notifier` 才能显示自定义图标：
   ```bash
   brew install terminal-notifier
   ```

2. **API 密钥安全**：请妥善保管你的 API 密钥，不要提交到代码仓库。

3. **网络要求**：需要能够访问配置的 API 接口地址。

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！