package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"paste-anywhere/server/internal/storage"
)

func printManagement(out io.Writer, s *storage.Store, devices bool) error {
	zone := os.Getenv("DISPLAY_TIMEZONE")
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return fmt.Errorf("DISPLAY_TIMEZONE 无效：%w", err)
	}
	stamp := func(ms int64) string { return formatTimestamp(ms, loc) }
	value := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}
	users, list, known, err := s.ManagementLists()
	if err != nil {
		return err
	}
	rows := [][]string{{"用户名", "设备数", "在线设备数", "最近内容时间", "创建时间"}}
	if devices {
		rows = [][]string{{"设备标识", "平台", "客户端版本", "在线状态", "最近 IP", "最后活跃时间", "配对时间"}}
		for _, d := range list {
			status := "未知"
			if known {
				status = "离线"
				if d.Online {
					status = "在线"
				}
			}
			rows = append(rows, []string{d.User + "-" + d.Name, value(d.Platform), value(d.Version), status, value(d.IP), stamp(d.LastActive), stamp(d.CreatedAt)})
		}
	} else {
		for _, u := range users {
			online := "未知"
			if known {
				online = strconv.Itoa(u.Online)
			}
			rows = append(rows, []string{u.Name, strconv.Itoa(u.Devices), online, stamp(u.RecentContent), stamp(u.CreatedAt)})
		}
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], tableWidth(cell))
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				if _, err := io.WriteString(out, "  "); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(out, cell); err != nil {
				return err
			}
			if i < len(row)-1 {
				if _, err := io.WriteString(out, strings.Repeat(" ", widths[i]-tableWidth(cell))); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(out, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func formatTimestamp(ms int64, loc *time.Location) string {
	if ms == 0 {
		return "—"
	}
	t := time.UnixMilli(ms).In(loc)
	_, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	suffix := fmt.Sprintf("%s%d", sign, offset/3600)
	if offset%3600 != 0 {
		suffix += fmt.Sprintf(":%02d", offset%3600/60)
	}
	return t.Format("2006-01-02 15:04:05") + " (" + suffix + ")"
}

// Data cells are ASCII (names, IPs, versions, dates); headers/statuses use CJK.
func tableWidth(s string) int {
	width := 0
	for _, r := range s {
		width++
		if r >= 0x2e80 && r <= 0x9fff {
			width++
		}
	}
	return width
}
