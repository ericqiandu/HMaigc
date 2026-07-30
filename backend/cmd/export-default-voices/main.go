package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
)

type defaultVoice struct {
	VoiceKey    string `json:"voiceKey"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Language    string `json:"language"`
}

var supportedLanguagePrefixes = map[string]bool{
	"English": true, "Japanese": true, "Korean": true, "Spanish": true, "French": true,
	"Portuguese": true, "German": true, "Russian": true, "Arabic": true, "Italian": true,
	"Turkish": true, "Dutch": true, "Ukrainian": true, "Vietnamese": true, "Indonesian": true,
	"Thai": true, "Polish": true, "Romanian": true, "Greek": true, "Czech": true,
	"Finnish": true, "Hindi": true, "Bulgarian": true, "Danish": true, "Hebrew": true,
	"Malay": true, "Persian": true, "Slovak": true, "Swedish": true, "Croatian": true,
	"Filipino": true, "Hungarian": true, "Norwegian": true, "Slovenian": true, "Catalan": true,
	"Nynorsk": true, "Tamil": true, "Afrikaans": true,
}

func main() {
	databasePath := flag.String("database", "", "只读 SQLite 数据库路径")
	outputPath := flag.String("output", "", "默认音色 JSON 输出路径")
	flag.Parse()
	if strings.TrimSpace(*databasePath) == "" || strings.TrimSpace(*outputPath) == "" {
		exitWithError("必须同时提供 -database 和 -output")
	}

	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(*databasePath)+"?mode=ro")
	if err != nil {
		exitWithError(fmt.Sprintf("打开数据库失败: %v", err))
	}
	defer func() {
		if err := db.Close(); err != nil {
			exitWithError(fmt.Sprintf("关闭数据库失败: %v", err))
		}
	}()
	rows, err := db.Query(`
		SELECT voice_key, display_name, COALESCE(description, '')
		FROM channel_voices
		WHERE kind = 'system' AND enabled = 1 AND provider_status IN ('active', 'pending_activation')
		ORDER BY voice_key
	`)
	if err != nil {
		exitWithError(fmt.Sprintf("读取系统音色失败: %v", err))
	}
	defer func() {
		if err := rows.Close(); err != nil {
			exitWithError(fmt.Sprintf("关闭查询结果失败: %v", err))
		}
	}()

	voices := make([]defaultVoice, 0, 320)
	for rows.Next() {
		var voice defaultVoice
		if err := rows.Scan(&voice.VoiceKey, &voice.DisplayName, &voice.Description); err != nil {
			exitWithError(fmt.Sprintf("读取系统音色字段失败: %v", err))
		}
		voice.Language = classifyLanguage(voice.VoiceKey, voice.DisplayName)
		voices = append(voices, voice)
	}
	if err := rows.Err(); err != nil {
		exitWithError(fmt.Sprintf("遍历系统音色失败: %v", err))
	}
	if len(voices) == 0 {
		exitWithError("系统音色目录为空，拒绝覆盖默认数据")
	}
	sort.SliceStable(voices, func(left int, right int) bool {
		leftRank := languageRank(voices[left].Language)
		rightRank := languageRank(voices[right].Language)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if voices[left].DisplayName != voices[right].DisplayName {
			return voices[left].DisplayName < voices[right].DisplayName
		}
		return voices[left].VoiceKey < voices[right].VoiceKey
	})

	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		exitWithError(fmt.Sprintf("创建输出目录失败: %v", err))
	}
	file, err := os.Create(*outputPath)
	if err != nil {
		exitWithError(fmt.Sprintf("创建输出文件失败: %v", err))
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(voices); err != nil {
		_ = file.Close()
		exitWithError(fmt.Sprintf("写入默认音色失败: %v", err))
	}
	if err := file.Close(); err != nil {
		exitWithError(fmt.Sprintf("保存默认音色失败: %v", err))
	}
	fmt.Printf("exported %d voices to %s\n", len(voices), *outputPath)
}

func classifyLanguage(voiceKey string, displayName string) string {
	switch {
	case strings.HasPrefix(voiceKey, "Chinese (Mandarin)_"):
		return "Chinese"
	case strings.HasPrefix(voiceKey, "Cantonese_"):
		return "Chinese,Yue"
	}
	prefix, _, found := strings.Cut(voiceKey, "_")
	if found && supportedLanguagePrefixes[prefix] {
		return prefix
	}
	if strings.IndexFunc(displayName, func(value rune) bool { return unicode.Is(unicode.Han, value) }) >= 0 {
		return "Chinese"
	}
	return "English"
}

func languageRank(language string) int {
	switch language {
	case "Chinese":
		return 0
	case "Chinese,Yue":
		return 1
	default:
		return 2
	}
}

func exitWithError(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
