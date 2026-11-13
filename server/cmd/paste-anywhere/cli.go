package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"paste-anywhere/server/internal/storage"
)

const usage = `用法：
  paste-anywhere serve [选项]
  paste-anywhere user create <用户名> [--db 路径]
  paste-anywhere user reset <用户名> [--db 路径]
  paste-anywhere user list [--db 路径]
  paste-anywhere user delete <用户名> [--db 路径]
  paste-anywhere device list [--db 路径]
  paste-anywhere device delete <用户名>-<设备名> [--db 路径]

用户名和设备名只能包含英文字母、数字，长度 1–128，区分大小写。
设备名在同一用户内唯一。删除用户会删除其设备及收到的内容。
重置配对码会撤销该用户全部设备，保留用户及在线内容。
数据库默认读取 PASTE_ANYWHERE_DB，否则使用 data/paste-anywhere.db。
list 输出为表格，其他管理输出为 JSON，不展示内部 ID。
DISPLAY_TIMEZONE 配置时间显示时区，默认 Asia/Shanghai。命令后加 --help 查看选项。
`

func runCLI(args []string, out, errOut io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	command, rest := args[0], args[1:]
	if command == "user" || command == "device" {
		if len(rest) == 0 || rest[0] == "--help" || rest[0] == "-h" {
			_, err := fmt.Fprint(out, usage)
			return err
		}
		command += " " + rest[0]
		rest = rest[1:]
	}
	wantArgs := 0
	switch command {
	case "serve", "user list", "device list":
	case "user create", "user reset", "user delete", "device delete":
		wantArgs = 1
	default:
		return fmt.Errorf("未知指令 %q；使用 --help 查看可用指令", command)
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(errOut)
	f.Usage = func() { fmt.Fprint(errOut, usage); f.PrintDefaults() }
	defaultDB := os.Getenv("PASTE_ANYWHERE_DB")
	if defaultDB == "" {
		defaultDB = "data/paste-anywhere.db"
	}
	db := f.String("db", defaultDB, "SQLite 数据库路径，必须与 serve 使用同一个文件")
	listen, history, retention, maxText, maxImage := "127.0.0.1:8080", 20, 7*24*time.Hour, 1024*1024, 16*1024*1024
	if command == "serve" {
		f.StringVar(&listen, "listen", listen, "HTTP 监听地址，HTTPS 由反向代理处理")
		f.IntVar(&history, "history", history, "每用户历史条目上限")
		f.DurationVar(&retention, "retention", retention, "条目保留时间")
		f.IntVar(&maxText, "max-text", maxText, "文本大小上限（字节）")
		f.IntVar(&maxImage, "max-image", maxImage, "图片大小上限（字节）")
	}
	// Go's flag parser stops at the first positional argument; allow options
	// before or after names while still rejecting unknown/missing options.
	var options, positional []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if arg == "--" {
			positional = append(positional, rest[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		options = append(options, arg)
		if strings.Contains(arg, "=") || arg == "-h" || arg == "--help" {
			continue
		}
		key := strings.TrimLeft(arg, "-")
		if f.Lookup(key) == nil {
			return fmt.Errorf("未知选项：%s", arg)
		}
		if i+1 == len(rest) || strings.HasPrefix(rest[i+1], "--") {
			return fmt.Errorf("选项 %s 缺少参数", arg)
		}
		i++
		options = append(options, rest[i])
	}
	if err := f.Parse(options); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(positional) != wantArgs {
		return fmt.Errorf("%s 需要 %d 个名称参数；使用 --help 查看用法", command, wantArgs)
	}
	if (command == "user create" || command == "user reset" || command == "user delete") && !storage.ValidName(positional[0]) {
		return storage.ErrInvalidName
	}
	if command == "device delete" {
		user, device, ok := strings.Cut(positional[0], "-")
		if !ok || !storage.ValidName(user) || !storage.ValidName(device) {
			return fmt.Errorf("设备标识格式应为 用户名-设备名，名称只能包含英文字母和数字")
		}
	}
	if maxText < 1 || maxText > 16*1024*1024 {
		return fmt.Errorf("max-text 必须在 1–16777216 字节之间")
	}
	if maxImage < 1 || maxImage > 64*1024*1024 {
		return fmt.Errorf("max-image 必须在 1–67108864 字节之间")
	}
	s, err := storage.Open(*db, history, retention)
	if err != nil {
		return err
	}
	defer s.Close()
	encoder := json.NewEncoder(out)
	switch command {
	case "serve":
		return serve(s, listen, maxText, maxImage)
	case "user list":
		return printManagement(out, s, false)
	case "user create", "user reset":
		var invite string
		if command == "user create" {
			_, invite, err = s.CreateUser(positional[0])
		} else {
			invite, err = s.ResetPairingCode(positional[0])
		}
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("未找到指定的用户：%s", positional[0])
		}
		if err != nil {
			return err
		}
		return encoder.Encode(struct {
			User   map[string]string `json:"user"`
			Invite string            `json:"invite"`
		}{map[string]string{"name": positional[0]}, invite})
	case "device list":
		return printManagement(out, s, true)
	case "user delete":
		err = s.DeleteUserByName(positional[0])
	case "device delete":
		err = s.DeleteDeviceByName(positional[0])
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("未找到指定的用户或设备：%s", positional[0])
	}
	if err != nil {
		return err
	}
	return encoder.Encode(map[string]string{"deleted": positional[0]})
}
