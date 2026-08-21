package service

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestRegistrationEmailContentUsesBrandingAndRealExpiry(t *testing.T) {
	content := buildRegistrationEmailContent("826491", 10*time.Minute, registrationEmailBranding{
		SiteName:  "HMaigc <商业版>",
		Slogan:    "让算力更有想象力！",
		Copyright: "© 2026 HMaigc. 保留所有权利。",
	})

	plainExpectations := []string{
		"HMaigc <商业版>",
		"让算力更有想象力！",
		"826 491",
		"10 分钟后失效",
		"请勿将验证码提供给其他任何人",
	}
	for _, expected := range plainExpectations {
		if !strings.Contains(content.PlainText, expected) {
			t.Fatalf("plain text email is missing %q\n%s", expected, content.PlainText)
		}
	}

	htmlExpectations := []string{
		"验证你的邮箱",
		"HMaigc &lt;商业版&gt;",
		"让算力更有想象力！",
		"826 491",
		"10 分钟后失效",
		"© 2026 HMaigc. 保留所有权利。",
	}
	for _, expected := range htmlExpectations {
		if !strings.Contains(content.HTML, expected) {
			t.Fatalf("HTML email is missing %q", expected)
		}
	}
	if strings.Contains(content.HTML, "HMaigc <商业版>") {
		t.Fatal("HTML email contains an unescaped configured site name")
	}
}

func TestBuildSMTPMessageProvidesPlainTextAndHTMLAlternatives(t *testing.T) {
	content := registrationEmailContent{
		PlainText: "验证码：826 491",
		HTML:      "<html><body><strong>826 491</strong></body></html>",
	}
	message, err := buildSMTPMessage(
		mail.Address{Name: "HMaigc", Address: "noreply@hmaigc.ai"},
		"member@example.com",
		"HMaigc 邮箱验证码",
		content,
	)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "multipart/alternative" || params["boundary"] == "" {
		t.Fatalf("unexpected content type: %q", parsed.Header.Get("Content-Type"))
	}

	parts := map[string]string{}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			t.Fatal(partErr)
		}
		partType, _, parseErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		body, readErr := io.ReadAll(quotedprintable.NewReader(part))
		if readErr != nil {
			t.Fatal(readErr)
		}
		parts[partType] = string(body)
	}

	if parts["text/plain"] != content.PlainText {
		t.Fatalf("unexpected plain text part: %q", parts["text/plain"])
	}
	if parts["text/html"] != content.HTML {
		t.Fatalf("unexpected HTML part: %q", parts["text/html"])
	}
}
