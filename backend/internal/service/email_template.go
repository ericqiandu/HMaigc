package service

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

type registrationEmailBranding struct {
	SiteName  string
	Slogan    string
	Copyright string
}

type registrationEmailContent struct {
	PlainText string
	HTML      string
}

func buildRegistrationEmailContent(code string, expiresIn time.Duration, branding registrationEmailBranding) registrationEmailContent {
	displayCode := formatRegistrationCode(code)
	expiryMinutes := int(expiresIn / time.Minute)
	plainText := fmt.Sprintf(`%s
%s

验证你的邮箱

你好，

感谢你使用 %s。
你正在创建账号，请使用以下验证码完成邮箱验证：

%s

验证码将在 %d 分钟后失效，请尽快完成验证。
为保障账号安全，请勿将验证码提供给其他任何人，包括客服人员。

如果这不是你的操作，请忽略这封邮件，无需进行任何处理。

%s`, branding.SiteName, branding.Slogan, branding.SiteName, displayCode, expiryMinutes, branding.Copyright)

	siteName := html.EscapeString(branding.SiteName)
	slogan := html.EscapeString(branding.Slogan)
	copyright := html.EscapeString(branding.Copyright)
	safeCode := html.EscapeString(displayCode)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>验证你的邮箱</title>
</head>
<body style="margin:0;padding:0;background:#f3f6fc;color:#101b36;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',Arial,sans-serif;">
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="width:100%%;background:#f3f6fc;">
    <tr>
      <td align="center" style="padding:40px 16px;">
        <table role="presentation" width="600" cellspacing="0" cellpadding="0" border="0" style="width:100%%;max-width:600px;background:#ffffff;border-radius:20px;box-shadow:0 18px 55px rgba(28,67,145,.12);overflow:hidden;">
          <tr>
            <td align="center" style="padding:48px 48px 20px;">
              <table role="presentation" cellspacing="0" cellpadding="0" border="0">
                <tr>
                  <td align="center" valign="middle" width="52" height="52" style="width:52px;height:52px;border-radius:14px;background:#2864f0;color:#ffffff;font-size:30px;font-weight:800;line-height:52px;">H</td>
                  <td style="padding-left:14px;color:#101b36;font-size:34px;font-weight:750;letter-spacing:-1px;">%s</td>
                </tr>
              </table>
              <p style="margin:16px 0 0;color:#667493;font-size:16px;line-height:24px;letter-spacing:2px;">%s</p>
            </td>
          </tr>
          <tr>
            <td style="padding:18px 48px 8px;">
              <h1 style="margin:0 0 30px;color:#101b36;font-size:34px;line-height:1.25;font-weight:800;letter-spacing:-1px;">验证你的邮箱</h1>
              <p style="margin:0 0 12px;color:#24314d;font-size:16px;line-height:1.8;">你好，</p>
              <p style="margin:0 0 8px;color:#24314d;font-size:16px;line-height:1.8;">感谢你使用 %s。</p>
              <p style="margin:0;color:#24314d;font-size:16px;line-height:1.8;">你正在创建账号，请使用以下验证码完成邮箱验证：</p>
            </td>
          </tr>
          <tr>
            <td style="padding:28px 48px 22px;">
              <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="width:100%%;background:#f1f5ff;border:1px solid #c9d8ff;border-radius:14px;">
                <tr>
                  <td align="center" style="padding:30px 20px;color:#2458e8;font-family:'SFMono-Regular',Consolas,'Liberation Mono',monospace;font-size:48px;line-height:1;font-weight:800;letter-spacing:8px;white-space:nowrap;">%s</td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td style="padding:0 48px 36px;">
              <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0">
                <tr>
                  <td width="32" valign="top" style="padding-top:2px;color:#2864f0;font-size:22px;">◷</td>
                  <td style="color:#24314d;font-size:15px;line-height:1.7;">验证码将在 <strong style="color:#2458e8;">%d 分钟后失效</strong>，请尽快完成验证。</td>
                </tr>
              </table>
              <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="margin-top:22px;background:#f7f9fe;border-radius:12px;">
                <tr>
                  <td width="32" valign="top" style="padding:18px 0 18px 18px;color:#2864f0;font-size:20px;">◇</td>
                  <td style="padding:16px 18px;color:#24314d;font-size:14px;line-height:1.7;">为保障账号安全，请勿将验证码提供给其他任何人，包括客服人员。</td>
                </tr>
              </table>
              <p style="margin:28px 0 0;padding-top:24px;border-top:1px solid #e6ebf5;color:#5e6b86;font-size:14px;line-height:1.8;">如果这不是你的操作，请忽略这封邮件，无需进行任何处理。</p>
            </td>
          </tr>
          <tr>
            <td align="center" style="padding:28px 48px 34px;background:#f7f9fe;">
              <p style="margin:0;color:#101b36;font-size:22px;font-weight:750;">%s</p>
              <p style="margin:8px 0 16px;color:#72809c;font-size:13px;letter-spacing:1px;">%s</p>
              <p style="margin:0;color:#8a96ac;font-size:12px;line-height:1.6;">%s</p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`, siteName, slogan, siteName, safeCode, expiryMinutes, siteName, slogan, copyright)

	return registrationEmailContent{PlainText: plainText, HTML: htmlBody}
}

func formatRegistrationCode(code string) string {
	if len(code) != 6 {
		return code
	}
	return code[:3] + " " + code[3:]
}

func buildSMTPMessage(from mail.Address, recipient string, subject string, content registrationEmailContent) ([]byte, error) {
	if strings.ContainsAny(recipient, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return nil, fmt.Errorf("邮件头字段不能包含换行")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writeEmailAlternative(writer, "text/plain; charset=UTF-8", content.PlainText); err != nil {
		return nil, err
	}
	if err := writeEmailAlternative(writer, "text/html; charset=UTF-8", content.HTML); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", from.String())
	fmt.Fprintf(&message, "To: %s\r\n", recipient)
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprint(&message, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", writer.Boundary())
	_, _ = message.Write(body.Bytes())
	return message.Bytes(), nil
}

func writeEmailAlternative(writer *multipart.Writer, contentType string, content string) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", contentType)
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	encoded := quotedprintable.NewWriter(part)
	if _, err := encoded.Write([]byte(content)); err != nil {
		_ = encoded.Close()
		return err
	}
	return encoded.Close()
}
