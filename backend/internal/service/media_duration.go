package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type mediaDurationProbe func(context.Context, io.Reader) (int64, error)

func (s *Service) authoritativeMediaDuration(kind string, body io.ReadSeeker) (int64, error) {
	if kind != "video" && kind != "audio" {
		return 0, nil
	}
	if body == nil {
		return 0, BadAuthRequest("媒体文件内容为空")
	}
	probe := s.mediaDurationProbe
	if probe == nil {
		probe = ffprobeMediaDuration
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	durationMs, err := probe(ctx, body)
	_, seekErr := body.Seek(0, io.SeekStart)
	if err != nil {
		return 0, BadAuthRequest("无法读取媒体真实时长：" + truncateRunes(err.Error(), 300))
	}
	if seekErr != nil {
		return 0, fmt.Errorf("重置媒体读取位置失败：%w", seekErr)
	}
	if durationMs <= 0 {
		return 0, BadAuthRequest("媒体真实时长无效")
	}
	return durationMs, nil
}

func ffprobeMediaDuration(ctx context.Context, body io.Reader) (int64, error) {
	temporary, err := os.CreateTemp("", "hmaigc-media-probe-*")
	if err != nil {
		return 0, fmt.Errorf("创建媒体探测临时文件失败：%w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := io.Copy(temporary, body); err != nil {
		_ = temporary.Close()
		return 0, fmt.Errorf("写入媒体探测临时文件失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return 0, fmt.Errorf("关闭媒体探测临时文件失败：%w", err)
	}
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", temporaryPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe 执行失败：%s", strings.TrimSpace(stderr.String()))
	}
	return parseMediaDurationMilliseconds(stdout.String())
}

func parseMediaDurationMilliseconds(value string) (int64, error) {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 {
		return 0, errors.New("ffprobe 未返回有效时长")
	}
	if seconds > float64(^uint64(0)>>1)/1000 {
		return 0, errors.New("媒体时长超出可表示范围")
	}
	return int64(seconds*1000 + 0.5), nil
}
