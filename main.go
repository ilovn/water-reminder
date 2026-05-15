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
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
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

var (
	lunchReminderActive  bool
	lunchReminderMu      sync.Mutex
	lunchPostponeTimer   *time.Timer
	lunchAutoRemindTimer *time.Timer
	lunchFirstRemind     bool
	lunchRemindCount     int

	dinnerReminderActive  bool
	dinnerReminderMu      sync.Mutex
	dinnerPostponeTimer   *time.Timer
	dinnerAutoRemindTimer *time.Timer
	dinnerFirstRemind     bool
	dinnerRemindCount     int
	dinnerSilentMode      int

	postponeMenuItem   *systray.MenuItem
	doneMenuItem       *systray.MenuItem
	postpone10MenuItem *systray.MenuItem
	postpone30MenuItem *systray.MenuItem
	doneDinnerMenuItem *systray.MenuItem
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
	MODEL_NAME = getEnvOrDefault("WATER_REMINDER_MODEL_NAME", "qwen3:32b")
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

var fallbackMessages = []string{
	"💧 身体在喊渴！快去倒杯水吧~",
	"🚰 你的细胞正在等水润，快去浇灌它们！",
	"🍵 喝水时间到！别让身体变成沙漠哦~",
	"💦 今天的下一杯水，你喝了吗？",
	"🥤 站起来活动一下，顺便倒杯水吧！",
	"🌊 水是生命之源，缺你可不行！",
	"💧 每一滴水都在感谢你的及时补充~",
	"🥛 牛奶很好，但水才是真爱！",
	"🧊 冰凉的清水，让你的思维更清醒！",
	"💧 小口慢饮，养生从一杯水开始~",
	"🌿 给身体浇浇水，让活力绽放开！",
	"💧 你的肝脏在等你，快去喝水！",
	"🚰 打开水龙头，健康跟着走~",
	"💧 八杯水计划，你完成几杯了？",
	"🍶 古人喝水养生，今人更需补水！",
	"💦 皮肤也需要水，喝出好气色~",
	"🌸 一杯清水，胜过万千饮料！",
	"💧 运动后要补水，静坐也要润喉~",
	"🥤 可乐很爽，但水才是正解！",
	"💧 清晨第一杯水，唤醒美好一天~",
	"🌈 彩虹再美，也不如一杯清水实在！",
	"💧 让血液流动起来，从一杯水开始！",
	"🫖 茶是茶，水是水，养生要分明~",
	"💧 喝水不忘挖井人，更要感谢自己！",
	"🌞 防晒重要，保湿更重要——多喝水！",
	"💧 你的大脑90%是水，快补充能量！",
	"🚿 洗澡能清洁身体内部——多喝水！",
	"💧 肚子饿了？也许只是渴了，先喝水！",
	"🍵 热茶暖身，冰水提神，你选哪个？",
	"💧 每隔半小时，起来走走去倒水~",
	"🌙 睡前少喝点，但也不能不喝哦！",
	"💧 办公桌旁放杯水，工作更有精神！",
	"🏃 运动前先补水，运动中要续水，运动后要喝水！",
	"💧 感冒了？多喝热水，这是真的有用！",
	"🍎 吃水果不等于喝水，两者都要有！",
	"💧 减肥期间更要喝水，代谢全靠它！",
	"🌿 绿色出行不如绿色饮水，更环保！",
	"💧 开会前喝杯水，发言更有底气！",
	"☕ 咖啡因利尿，喝完记得补水哦！",
	"💧 吃完重口味，喝点水冲淡一下~",
	"🍜 汤面汤面，汤多水少，记得额外补水！",
	"💧 空调房里水分流失快，多喝点！",
	"🌡️ 天气热出汗多，水必须补够！",
	"💧 暖气房干燥，加湿器不如多喝水！",
	"🧠 脑子转不动？也许只是缺水了！",
}

var lunchFallbackMessages = []string{
	"🍛 午餐时间快到啦！该订餐啦~",
	"🥡 十一点啦！还不赶紧订午餐？",
	"🍱 肚子在咕咕叫，快去选外卖！",
	"🍔 午餐订餐时间到，选个好吃的吧！",
	"🌮 今天中午吃点啥？赶紧下单！",
	"🥗 别等到饿了才订餐，提前行动！",
	"🍝 午餐时间即将到来，订餐走起！",
	"🍣 赶紧订午餐，不然要排队啦！",
	"🌯 十一点五十五，订餐别耽误！",
	"🍲 美味午餐在等你，快去下单！",
}

func getRandomFallbackMessage() string {
	return fallbackMessages[time.Now().UnixNano()%int64(len(fallbackMessages))]
}

func getRandomLunchFallbackMessage() string {
	return lunchFallbackMessages[time.Now().UnixNano()%int64(len(lunchFallbackMessages))]
}

func cleanResponse(content string) string {
	content = strings.TrimSpace(content)

	if idx := strings.Index(content, "\""); idx != -1 {
		endIdx := strings.LastIndex(content, "\"")
		if endIdx > idx {
			return strings.TrimSpace(content[idx+1 : endIdx])
		}
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) <= 80 && !strings.Contains(line, ":") && !strings.Contains(line, "：") {
			return line
		}
	}

	if len(content) <= 80 {
		return content
	}
	return content[:80]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	noGUIFlag := flag.Bool("nogui", false, "不启动图形界面，仅运行HTTP服务器用于测试")
	flag.Parse()

	beeep.AppName = "喝了么"

	if *helpFlag {
		printHelp()
		return
	}

	go startHTTPServer()

	if *noGUIFlag {
		fmt.Printf("[%s] 【模式】无GUI模式运行，按 Ctrl+C 退出\n", time.Now().Format("15:04:05"))
		select {}
	}

	systray.Run(onReady, onExit)
}

func startHTTPServer() {
	http.HandleFunc("/lunch/trigger", handleLunchTrigger)
	http.HandleFunc("/lunch/postpone", handleLunchPostpone)
	http.HandleFunc("/lunch/done", handleLunchDone)

	fmt.Printf("[%s] 【HTTP 服务器】启动于 http://localhost:8085\n", time.Now().Format("15:04:05"))
	if err := http.ListenAndServe(":8085", nil); err != nil && err != http.ErrServerClosed {
		fmt.Printf("[%s] 【HTTP 服务器错误】%v\n", time.Now().Format("15:04:05"), err)
	}
}

func handleLunchTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go remindLunch()

	fmt.Printf("[%s] 【午餐提醒】通过HTTP接口触发\n", time.Now().Format("15:04:05"))
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "午餐提醒已触发")
}

func handleLunchPostpone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lunchReminderMu.Lock()
	if lunchReminderActive {
		lunchReminderActive = false
		if lunchPostponeTimer != nil {
			lunchPostponeTimer.Stop()
		}
		lunchPostponeTimer = time.AfterFunc(5*time.Minute, func() {
			go remindLunch()
		})
		fmt.Printf("[%s] 【午餐提醒】用户选择5分钟后再提醒\n", time.Now().Format("15:04:05"))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "已推迟5分钟")
	} else {
		fmt.Printf("[%s] 【午餐提醒】无活动提醒，跳过推迟操作\n", time.Now().Format("15:04:05"))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "当前无活动的午餐提醒，可先调用 /lunch/trigger 触发提醒")
	}
	lunchReminderMu.Unlock()
}

func handleLunchDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lunchReminderMu.Lock()
	lunchReminderActive = false
	if lunchPostponeTimer != nil {
		lunchPostponeTimer.Stop()
		lunchPostponeTimer = nil
		fmt.Printf("[%s] 【午餐提醒】用户已订餐，取消推迟定时器\n", time.Now().Format("15:04:05"))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "已取消推迟提醒，祝你用餐愉快！")
	} else {
		fmt.Printf("[%s] 【午餐提醒】用户已订餐\n", time.Now().Format("15:04:05"))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "祝你用餐愉快！")
	}
	lunchReminderMu.Unlock()
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
    sed -i.bak 's/qwen3:32b/qwen3:32b/g' main.go

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
	mMorning := systray.AddMenuItem("上班提醒", "测试上班提醒")
	mLunch := systray.AddMenuItem("午餐提醒", "测试午餐订餐提醒")
	mDinner := systray.AddMenuItem("下班提醒", "测试下班提醒")
	systray.AddSeparator()

	postponeMenuItem = systray.AddMenuItem("🍱 我在忙，五分钟后提醒", "5分钟后再次提醒")
	doneMenuItem = systray.AddMenuItem("✅ 已经订了", "确认已订餐")

	postponeMenuItem.Hide()
	doneMenuItem.Hide()

	systray.AddSeparator()

	postpone10MenuItem = systray.AddMenuItem("⏰ 再忙10分钟", "10分钟后再次提醒")
	postpone30MenuItem = systray.AddMenuItem("⏱️ 再忙30分钟", "30分钟后再次提醒")
	doneDinnerMenuItem = systray.AddMenuItem("🏠 已经下班", "确认已下班")

	postpone10MenuItem.Hide()
	postpone30MenuItem.Hide()
	doneDinnerMenuItem.Hide()

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "关闭程序")

	go func() {
		for {
			select {
			case <-mQuit.ClickedCh:
				systray.Quit()
			case <-mNow.ClickedCh:
				go remind()
			case <-mMorning.ClickedCh:
				go remindMorning()
			case <-mLunch.ClickedCh:
				go remindLunch()
			case <-mDinner.ClickedCh:
				go remindDinner()
			case <-postponeMenuItem.ClickedCh:
				go handlePostponeClick()
			case <-doneMenuItem.ClickedCh:
				go handleDoneClick()
			case <-postpone10MenuItem.ClickedCh:
				go handleDinnerPostponeClick(10)
			case <-postpone30MenuItem.ClickedCh:
				go handleDinnerPostponeClick(30)
			case <-doneDinnerMenuItem.ClickedCh:
				go handleDinnerDoneClick()
			}
		}
	}()

	// 上班提醒定时器（每天 09:00，仅工作日）
	go func() {
		for {
			now := time.Now()
			target := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
			if now.After(target) {
				target = target.Add(24 * time.Hour)
			}
			timer := time.NewTimer(time.Until(target))
			<-timer.C

			if isWorkday(time.Now()) {
				go remindMorning()
			}
		}
	}()

	// 喝水提醒定时器（按配置间隔，仅工作日）
	go func() {
		ticker := time.NewTicker(REMIND_GAP)
		for now := range ticker.C {
			if isWorkday(now) {
				go remind()
			}
		}
	}()

	// 午餐提醒定时器（每天 10:55，仅工作日）
	go func() {
		for {
			now := time.Now()
			target := time.Date(now.Year(), now.Month(), now.Day(), 10, 55, 0, 0, now.Location())
			if now.After(target) {
				target = target.Add(24 * time.Hour)
			}
			timer := time.NewTimer(time.Until(target))
			<-timer.C

			if isWorkday(time.Now()) {
				go remindLunch()
			}
		}
	}()

	// 下班提醒定时器（每天 18:05，仅工作日）
	go func() {
		for {
			now := time.Now()
			target := time.Date(now.Year(), now.Month(), now.Day(), 18, 5, 0, 0, now.Location())
			if now.After(target) {
				target = target.Add(24 * time.Hour)
			}
			timer := time.NewTimer(time.Until(target))
			<-timer.C

			if isWorkday(time.Now()) {
				go remindDinner()
			}
		}
	}()

	go remind() // 启动先提醒一次
}

func isWorkday(t time.Time) bool {
	weekday := t.Weekday()
	return weekday != time.Saturday && weekday != time.Sunday
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
	iconData := getNotificationIconData()
	_ = beeep.Notify("喝水时间到！", msg, iconData)
	fmt.Printf("[%s] 提醒内容: %s\n", time.Now().Format("15:04"), msg)
}

func remindOffWork() {
	fmt.Printf("[%s] 【下班提醒】正在生成下班提醒文案...\n", time.Now().Format("15:04:05"))
	msg := getOffWorkLLMMessage()
	if msg == "" {
		fmt.Printf("[%s] 【下班提醒】LLM 不可用，跳过下班提醒\n", time.Now().Format("15:04:05"))
		return
	}
	iconData := getNotificationIconData()
	_ = beeep.Notify("下班时间到！", msg, iconData)
	fmt.Printf("[%s] 下班提醒内容: %s\n", time.Now().Format("15:04"), msg)
}

func getOffWorkLLMMessage() string {
	fn := func() (string, error) {
		config := openai.DefaultConfig(API_KEY)
		config.BaseURL = BASE_URL

		client := openai.NewClientWithConfig(config)

		req := openai.ChatCompletionRequest{
			Model: MODEL_NAME,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "你是一个简洁的提醒助手。只返回一句简短幽默的下班提醒语，不超过20字，直接给出结果，不要解释，不要列表，不要选项。",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: "下班提醒",
				},
			},
			ChatTemplateKwargs: map[string]any{
				"enable_thinking": false,
			},
		}

		fmt.Printf("[%s] 【下班 LLM 请求】(推理已禁用)\n", time.Now().Format("15:04:05"))

		ctx, cancel := context.WithTimeout(context.Background(), REQUEST_TIMEOUT)
		defer cancel()
		resp, err := client.CreateChatCompletion(ctx, req)

		if err != nil {
			fmt.Printf("[%s] 【下班 LLM 错误】%v\n", time.Now().Format("15:04:05"), err)
			return "", err
		}

		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("无返回结果")
		}

		content := resp.Choices[0].Message.Content
		if content == "" {
			return "", fmt.Errorf("响应内容为空")
		}
		return cleanResponse(content), nil
	}

	result, err := retryWithBackoff(MAX_RETRIES, INITIAL_DELAY, MAX_DELAY, fn)
	if err != nil {
		fmt.Printf("[%s] 【下班 LLM 最终失败】%v\n", time.Now().Format("15:04:05"), err)
		return ""
	}
	return result
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
					Content: "你是一个简洁的提醒助手。只返回一句简短幽默的喝水提醒语，不超过20字，直接给出结果，不要解释，不要列表，不要选项。",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: "喝水提醒",
				},
			},
			ChatTemplateKwargs: map[string]any{
				"enable_thinking": false,
			},
		}

		fmt.Printf("[%s] 【LLM 请求】(推理已禁用)\n", time.Now().Format("15:04:05"))
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
		return cleanResponse(content), nil
	}

	result, err := retryWithBackoff(MAX_RETRIES, INITIAL_DELAY, MAX_DELAY, fn)
	if err != nil {
		fmt.Printf("[%s] 【LLM 最终失败】%v\n", time.Now().Format("15:04:05"), err)
		fmt.Printf("[%s] 【备用方案】使用内置随机提醒语\n", time.Now().Format("15:04:05"))
		return getRandomFallbackMessage()
	}
	return result
}

func remindLunch() {
	lunchReminderMu.Lock()

	now := time.Now()
	hour, minute := now.Hour(), now.Minute()

	if hour >= 11 && minute >= 55 {
		lunchReminderActive = false
		lunchRemindCount = 0
		lunchFirstRemind = false
		stopLunchTimers()

		iconData := getNotificationIconData()
		_ = beeep.Notify("午餐时间过了 😢", "好吧...看来你今天不想订餐了，祝你下午工作愉快！", iconData)
		fmt.Printf("[%s] 【午餐提醒】幽怨提示：用户一直没订餐\n", time.Now().Format("15:04:05"))
		lunchReminderMu.Unlock()
		return
	}

	lunchReminderActive = true
	lunchRemindCount++

	if !lunchFirstRemind {
		lunchFirstRemind = true
	}

	lunchReminderMu.Unlock()

	msg := getLunchLLMMessage()
	iconData := getNotificationIconData()

	fmt.Printf("[%s] 【午餐提醒】第%d次提醒: %s\n", time.Now().Format("15:04:05"), lunchRemindCount, msg)

	postponeMenuItem.Show()
	doneMenuItem.Show()

	var notificationMsg string
	if runtime.GOOS == "windows" {
		notificationMsg = fmt.Sprintf("%s\n\n💡 点击托盘图标选择操作", msg)
	} else {
		notificationMsg = fmt.Sprintf("%s\n\n💡 点击托盘图标选择:\n• 我在忙，五分钟后提醒\n• 已经订了", msg)
	}
	_ = beeep.Notify("午餐时间到！", notificationMsg, iconData)

	setAutoRemindTimer()
}

func setAutoRemindTimer() {
	lunchReminderMu.Lock()
	defer lunchReminderMu.Unlock()

	if lunchAutoRemindTimer != nil {
		lunchAutoRemindTimer.Stop()
	}

	var delay time.Duration
	if lunchFirstRemind && lunchRemindCount == 1 {
		delay = 5 * time.Minute
	} else {
		delay = 10 * time.Minute
	}

	lunchAutoRemindTimer = time.AfterFunc(delay, func() {
		go remindLunch()
	})
	fmt.Printf("[%s] 【午餐提醒】设置自动提醒，%d分钟后\n", time.Now().Format("15:04:05"), delay/time.Minute)
}

func stopLunchTimers() {
	if lunchPostponeTimer != nil {
		lunchPostponeTimer.Stop()
		lunchPostponeTimer = nil
	}
	if lunchAutoRemindTimer != nil {
		lunchAutoRemindTimer.Stop()
		lunchAutoRemindTimer = nil
	}
}

func handlePostponeClick() {
	lunchReminderMu.Lock()
	stopLunchTimers()

	lunchPostponeTimer = time.AfterFunc(5*time.Minute, func() {
		go remindLunch()
	})
	fmt.Printf("[%s] 【午餐提醒】用户选择5分钟后再提醒\n", time.Now().Format("15:04:05"))

	postponeMenuItem.Hide()
	doneMenuItem.Hide()

	iconData := getNotificationIconData()
	_ = beeep.Notify("提醒已推迟", "5分钟后再次提醒您订餐", iconData)
	lunchReminderMu.Unlock()
}

func handleDoneClick() {
	lunchReminderMu.Lock()
	lunchReminderActive = false
	lunchRemindCount = 0
	lunchFirstRemind = false
	stopLunchTimers()

	fmt.Printf("[%s] 【午餐提醒】用户已订餐\n", time.Now().Format("15:04:05"))

	postponeMenuItem.Hide()
	doneMenuItem.Hide()

	lunchReminderMu.Unlock()

	iconData := getNotificationIconData()
	_ = beeep.Notify("用餐愉快！", "祝你用餐愉快！", iconData)
}

func getLunchLLMMessage() string {
	fn := func() (string, error) {
		config := openai.DefaultConfig(API_KEY)
		config.BaseURL = BASE_URL

		client := openai.NewClientWithConfig(config)

		req := openai.ChatCompletionRequest{
			Model: MODEL_NAME,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "你是一个简洁的提醒助手。只返回一句简短幽默的午餐订餐提醒语，不超过20字，直接给出结果，不要解释，不要列表，不要选项。",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: "午餐订餐提醒",
				},
			},
			ChatTemplateKwargs: map[string]any{
				"enable_thinking": false,
			},
		}

		fmt.Printf("[%s] 【午餐 LLM 请求】(推理已禁用)\n", time.Now().Format("15:04:05"))
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
			fmt.Printf("[%s] 【午餐 LLM 错误】耗时: %v, 错误: %v\n", time.Now().Format("15:04:05"), elapsed, err)
			return "", err
		}

		fmt.Printf("[%s] 【午餐 LLM 响应】耗时: %v\n", time.Now().Format("15:04:05"), elapsed)
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
		return cleanResponse(content), nil
	}

	result, err := retryWithBackoff(MAX_RETRIES, INITIAL_DELAY, MAX_DELAY, fn)
	if err != nil {
		fmt.Printf("[%s] 【午餐 LLM 最终失败】%v\n", time.Now().Format("15:04:05"), err)
		fmt.Printf("[%s] 【备用方案】使用内置随机提醒语\n", time.Now().Format("15:04:05"))
		return getRandomLunchFallbackMessage()
	}
	return result
}

func remindDinner() {
	dinnerReminderMu.Lock()

	now := time.Now()
	hour := now.Hour()

	if hour >= 22 {
		dinnerReminderActive = false
		dinnerRemindCount = 0
		dinnerFirstRemind = false
		dinnerSilentMode = 0
		stopDinnerTimers()

		iconData := getNotificationIconData()
		_ = beeep.Notify("看来你今天不下班了 😢", "注意身体！工作再忙也要照顾好自己，早点休息！", iconData)
		fmt.Printf("[%s] 【下班提醒】幽怨提示：用户一直没下班\n", time.Now().Format("15:04:05"))
		dinnerReminderMu.Unlock()
		return
	}

	dinnerReminderActive = true
	dinnerRemindCount++

	if !dinnerFirstRemind {
		dinnerFirstRemind = true
	}

	dinnerReminderMu.Unlock()

	msg := getOffWorkLLMMessage()
	if msg == "" {
		fmt.Printf("[%s] 【下班提醒】LLM 不可用，使用备用文案\n", time.Now().Format("15:04:05"))
		msg = "工作辛苦了，该下班啦！"
	}
	iconData := getNotificationIconData()

	fmt.Printf("[%s] 【下班提醒】第%d次提醒: %s\n", time.Now().Format("15:04:05"), dinnerRemindCount, msg)

	postpone10MenuItem.Show()
	postpone30MenuItem.Show()
	doneDinnerMenuItem.Show()

	var notificationMsg string
	if runtime.GOOS == "windows" {
		notificationMsg = fmt.Sprintf("%s\n\n💡 点击托盘图标选择操作", msg)
	} else {
		notificationMsg = fmt.Sprintf("%s\n\n💡 点击托盘图标选择:\n• 再忙10分钟\n• 再忙30分钟\n• 已经下班", msg)
	}
	_ = beeep.Notify("下班时间到！", notificationMsg, iconData)

	setDinnerAutoRemindTimer()
}

func setDinnerAutoRemindTimer() {
	dinnerReminderMu.Lock()
	defer dinnerReminderMu.Unlock()

	if dinnerAutoRemindTimer != nil {
		dinnerAutoRemindTimer.Stop()
	}

	var delay time.Duration
	if dinnerSilentMode == 10 {
		delay = 10 * time.Minute
	} else if dinnerSilentMode == 30 {
		delay = 30 * time.Minute
	} else if dinnerFirstRemind && dinnerRemindCount == 1 {
		delay = 10 * time.Minute
	} else {
		delay = 30 * time.Minute
	}

	dinnerAutoRemindTimer = time.AfterFunc(delay, func() {
		go remindDinner()
	})
	fmt.Printf("[%s] 【下班提醒】设置自动提醒，%d分钟后\n", time.Now().Format("15:04:05"), delay/time.Minute)
}

func stopDinnerTimers() {
	if dinnerPostponeTimer != nil {
		dinnerPostponeTimer.Stop()
		dinnerPostponeTimer = nil
	}
	if dinnerAutoRemindTimer != nil {
		dinnerAutoRemindTimer.Stop()
		dinnerAutoRemindTimer = nil
	}
}

func handleDinnerPostponeClick(minutes int) {
	dinnerReminderMu.Lock()
	stopDinnerTimers()

	dinnerSilentMode = minutes
	dinnerPostponeTimer = time.AfterFunc(time.Duration(minutes)*time.Minute, func() {
		dinnerSilentMode = 0
		go remindDinner()
	})
	fmt.Printf("[%s] 【下班提醒】用户选择%d分钟后再提醒\n", time.Now().Format("15:04:05"), minutes)

	postpone10MenuItem.Hide()
	postpone30MenuItem.Hide()
	doneDinnerMenuItem.Hide()

	iconData := getNotificationIconData()
	_ = beeep.Notify("休息一下", fmt.Sprintf("%d分钟后再次提醒您下班", minutes), iconData)
	dinnerReminderMu.Unlock()
}

func handleDinnerDoneClick() {
	dinnerReminderMu.Lock()
	dinnerReminderActive = false
	dinnerRemindCount = 0
	dinnerFirstRemind = false
	dinnerSilentMode = 0
	stopDinnerTimers()

	fmt.Printf("[%s] 【下班提醒】用户已下班\n", time.Now().Format("15:04:05"))

	postpone10MenuItem.Hide()
	postpone30MenuItem.Hide()
	doneDinnerMenuItem.Hide()

	dinnerReminderMu.Unlock()

	iconData := getNotificationIconData()
	_ = beeep.Notify("下班愉快！", "祝你度过一个愉快的夜晚！", iconData)
}

func remindMorning() {
	fmt.Printf("[%s] 【上班提醒】新的一天，元气满满~\n", time.Now().Format("15:04:05"))

	iconData := getNotificationIconData()
	_ = beeep.Notify("早安！", "新的一天，元气满满~", iconData)
}
