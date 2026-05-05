package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	"github.com/nfnt/resize"
	"github.com/sashabaranov/go-openai"
	"github.com/sergeymakinen/go-ico"
)

//go:embed water_reminder_logo2.png
var iconFS embed.FS

// --- 配置变量 ---
// 配置优先级（从高到低）：
// 1. 运行时环境变量
// 2. .env 文件
// 3. 编译时占位符注入的值
// 4. 代码默认值
var (
	API_KEY         string                       // 大模型 API 密钥
	BASE_URL        string                       // API 接口地址
	MODEL_NAME      string                       // 模型名称
	REMIND_GAP      time.Duration                // 提醒间隔
	MAX_RETRIES     int                          // 最大重试次数
	INITIAL_DELAY   time.Duration                // 初始重试间隔
	MAX_DELAY       time.Duration                // 最大重试间隔
	REQUEST_TIMEOUT time.Duration                // 请求超时时间
	ICON_PATH       = "water_reminder_logo2.png" // 应用图标路径（嵌入到二进制中）
)

func init() {
	loadEnvFile()
	initConfig()
}

func loadEnvFile() {
	envPath := ".env"
	data, err := os.ReadFile(envPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("[%s] 警告: 读取 .env 文件失败: %v\n", time.Now().Format("15:04:05"), err)
		}
		return
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.TrimPrefix(strings.TrimSuffix(value, `"`), `"`)
		value = strings.TrimPrefix(strings.TrimSuffix(value, `'`), `'`)

		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	fmt.Printf("[%s] 已加载 .env 文件配置\n", time.Now().Format("15:04:05"))
}

func initConfig() {
	API_KEY = getEnvOrDefault("WATER_REMINDER_API_KEY", "__API_KEY_PLACEHOLDER__")
	BASE_URL = getEnvOrDefault("WATER_REMINDER_BASE_URL", "__BASE_URL_PLACEHOLDER__")
	MODEL_NAME = getEnvOrDefault("WATER_REMINDER_MODEL_NAME", "__MODEL_NAME_PLACEHOLDER__")
	REMIND_GAP = getDurationEnv("WATER_REMINDER_REMIND_GAP", 35*time.Minute)
	MAX_RETRIES = getIntEnv("WATER_REMINDER_MAX_RETRIES", 3)
	INITIAL_DELAY = getDurationEnv("WATER_REMINDER_INITIAL_DELAY", 1*time.Second)
	MAX_DELAY = getDurationEnv("WATER_REMINDER_MAX_DELAY", 5*time.Second)
	REQUEST_TIMEOUT = getDurationEnv("WATER_REMINDER_TIMEOUT", 120*time.Second)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		fmt.Sscanf(value, "%d", &result)
		return result
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func pngToIco(pngData []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode PNG: %w", err)
	}

	iconSizes := []int{16, 24, 32, 48, 64, 128}
	var icons []image.Image

	for _, size := range iconSizes {
		resized := resize.Resize(uint(size), uint(size), img, resize.Bicubic)
		icons = append(icons, resized)
	}

	var buf bytes.Buffer
	if err := ico.EncodeAll(&buf, icons); err != nil {
		return nil, fmt.Errorf("failed to encode ICO: %w", err)
	}

	return buf.Bytes(), nil
}

func main() {
	helpFlag := flag.Bool("help", false, "显示帮助信息")
	flag.Parse()

	beeep.AppName = "喝了么"

	if *helpFlag {
		printHelp()
		return
	}

	systray.Run(onReady, onExit)
}

func printHelp() {
	helpText := `喝了么 - 喝水提醒工具

用法:
  water-reminder [选项]

选项:
  -help          显示帮助信息

功能特性:
  - 定时提醒喝水（默认35分钟一次）
  - 使用大模型生成有趣的提醒文案
  - 系统通知推送
  - 菜单栏常驻图标

配置说明:
  配置优先级（从高到低）:
    1. 运行时环境变量
    2. 当前目录下的 .env 文件
    3. 编译时通过占位符注入的值
    4. 代码默认值

  环境变量:
    WATER_REMINDER_API_KEY      - 大模型 API 密钥
    WATER_REMINDER_BASE_URL     - API 接口地址
    WATER_REMINDER_MODEL_NAME   - 模型名称
    WATER_REMINDER_REMIND_GAP   - 提醒间隔时间 (如: 1m, 5m, 1h)
    WATER_REMINDER_MAX_RETRIES  - 最大重试次数 (默认: 3)
    WATER_REMINDER_INITIAL_DELAY - 初始重试间隔 (如: 1s, 2s)
    WATER_REMINDER_MAX_DELAY    - 最大重试间隔 (如: 5s, 10s)
    WATER_REMINDER_TIMEOUT      - 请求超时时间 (如: 60s, 120s)

  时间格式说明:
    支持 Go 标准时间格式，如:
    - 1s (1秒), 5s (5秒)
    - 1m (1分钟), 5m (5分钟)
    - 1h (1小时)

  .env 文件格式:
    在应用程序同级目录创建 .env 文件，格式如下:
    WATER_REMINDER_API_KEY=your-api-key
    WATER_REMINDER_BASE_URL=https://your-api-url
    WATER_REMINDER_MODEL_NAME=qwen3:32b
    WATER_REMINDER_REMIND_GAP=35m

打包说明:
  编译前可通过 sed 替换占位符设置默认值:
    sed -i.bak 's/__API_KEY_PLACEHOLDER__/your-api-key/g' main.go
    sed -i.bak 's/__BASE_URL_PLACEHOLDER__/https://your-api-url/g' main.go
    sed -i.bak 's/__MODEL_NAME_PLACEHOLDER__/qwen3:32b/g' main.go

示例:
  ./water-reminder                              # 使用默认配置启动
  ./water-reminder -help                        # 显示帮助
  WATER_REMINDER_API_KEY=sk-xxx ./water-reminder  # 设置 API 密钥后启动
  export WATER_REMINDER_REMIND_GAP=5m           # 设置提醒间隔为5分钟
  export WATER_REMINDER_MODEL_NAME=deepseek-chat # 设置使用的模型
  echo "WATER_REMINDER_API_KEY=sk-xxx" > .env   # 使用 .env 文件配置
`
	fmt.Fprint(os.Stdout, helpText)
}

func onReady() {
	systray.SetTitle("喝了么")
	systray.SetTooltip("喝水提醒工具")

	iconData, err := fs.ReadFile(iconFS, ICON_PATH)
	if err != nil {
		fmt.Printf("[%s] 警告: 无法读取嵌入的图标文件: %v\n", time.Now().Format("15:04:05"), err)
		return
	}

	if runtime.GOOS == "windows" {
		icoData, err := pngToIco(iconData)
		if err != nil {
			fmt.Printf("[%s] 警告: 无法将 PNG 转换为 ICO: %v\n", time.Now().Format("15:04:05"), err)
			return
		}
		systray.SetIcon(icoData)
	} else {
		systray.SetIcon(iconData)
	}

	mNow := systray.AddMenuItem("立即提醒", "立刻生成一条提醒")
	mQuit := systray.AddMenuItem("退出", "关闭程序")

	go func() {
		for {
			select {
			case <-mQuit.ClickedCh:
				systray.Quit()
			case <-mNow.ClickedCh:
				go remind()
			}
		}
	}()

	// 定时逻辑
	go func() {
		ticker := time.NewTicker(REMIND_GAP)
		for range ticker.C {
			remind()
		}
	}()

	go remind() // 启动先提醒一次
}

func onExit() {}

var cachedIconData []byte

func getNotificationIconData() []byte {
	if cachedIconData != nil {
		return cachedIconData
	}

	iconData, err := fs.ReadFile(iconFS, ICON_PATH)
	if err != nil {
		fmt.Printf("[%s] 警告: 无法读取嵌入的图标文件: %v\n", time.Now().Format("15:04:05"), err)
		return nil
	}

	cachedIconData = iconData
	return iconData
}

func retryWithBackoff(maxRetries int, initialDelay, maxDelay time.Duration, fn func() (string, error)) (string, error) {
	delay := initialDelay
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err

		if attempt < maxRetries {
			fmt.Printf("[%s] 【LLM 重试】第 %d 次尝试失败，等待 %v 后重试...\n",
				time.Now().Format("15:04:05"), attempt, delay)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * 1.5)
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}

	return "", fmt.Errorf("已尝试 %d 次，均失败: %w", maxRetries, lastErr)
}

func remind() {
	msg := getLLMMessage()
	// 调用系统通知，使用嵌入的图标数据（[]byte）
	iconData := getNotificationIconData()
	_ = beeep.Notify("喝水时间到！", msg, iconData)
	fmt.Printf("[%s] 提醒内容: %s\n", time.Now().Format("15:04"), msg)
}

// 使用 OpenAI 兼容接口生成文案
func getLLMMessage() string {
	fn := func() (string, error) {
		config := openai.DefaultConfig(API_KEY)
		config.BaseURL = BASE_URL

		client := openai.NewClientWithConfig(config)

		req := openai.ChatCompletionRequest{
			Model: MODEL_NAME,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "你是一个贴心的健康助手。请生成一句短小、幽默且富有创意的喝水提醒语,字数20字以内。",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: "请写一句简短、有趣的喝水提醒。",
				},
			},
		}

		fmt.Printf("[%s] 【LLM 请求】\n", time.Now().Format("15:04:05"))
		if reqBody, err := json.MarshalIndent(req, "", "  "); err == nil {
			fmt.Printf("请求体: %s\n", string(reqBody))
		} else {
			fmt.Printf("模型: %s, 消息数: %d\n", req.Model, len(req.Messages))
		}

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), REQUEST_TIMEOUT)
		defer cancel()
		resp, err := client.CreateChatCompletion(ctx, req)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("[%s] 【LLM 错误】耗时: %v, 错误: %v\n", time.Now().Format("15:04:05"), elapsed, err)
			return "", err
		}

		fmt.Printf("[%s] 【LLM 响应】耗时: %v\n", time.Now().Format("15:04:05"), elapsed)
		if len(resp.Choices) == 0 {
			fmt.Println("响应: 无返回结果")
			return "", fmt.Errorf("无返回结果")
		}

		if respBody, err := json.MarshalIndent(resp, "", "  "); err == nil {
			fmt.Printf("响应体: %s\n", string(respBody))
		} else {
			fmt.Printf("响应: %s\n", resp.Choices[0].Message.Content)
		}

		content := resp.Choices[0].Message.Content
		if content == "" {
			fmt.Println("警告: 响应内容为空")
			return "", fmt.Errorf("响应内容为空")
		}
		return content, nil
	}

	result, err := retryWithBackoff(MAX_RETRIES, INITIAL_DELAY, MAX_DELAY, fn)
	if err != nil {
		fmt.Printf("[%s] 【LLM 最终失败】%v\n", time.Now().Format("15:04:05"), err)
		return "为了防止脑部逻辑溢出，请立即摄入 H2O。"
	}
	return result
}
