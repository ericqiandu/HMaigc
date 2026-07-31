package opsprotocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderTimestamp = "X-HMaigc-Ops-Timestamp"
	HeaderNonce     = "X-HMaigc-Ops-Nonce"
	HeaderSignature = "X-HMaigc-Ops-Signature"
)

func Signature(secret []byte, method string, requestURI string, timestamp string, nonce string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	canonical := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		requestURI,
		timestamp,
		nonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(secret []byte, method string, requestURI string, timestamp string, nonce string, body []byte, signature string, now time.Time, allowedSkew time.Duration) error {
	if len(secret) < 32 {
		return errors.New("运维控制器共享密钥长度不足")
	}
	if strings.TrimSpace(nonce) == "" || len(nonce) > 128 {
		return errors.New("运维请求 nonce 无效")
	}
	unixSeconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return errors.New("运维请求时间戳无效")
	}
	requestTime := time.Unix(unixSeconds, 0)
	if requestTime.Before(now.Add(-allowedSkew)) || requestTime.After(now.Add(allowedSkew)) {
		return fmt.Errorf("运维请求已过期")
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("运维请求签名格式无效")
	}
	expected, err := hex.DecodeString(Signature(secret, method, requestURI, timestamp, nonce, body))
	if err != nil {
		return err
	}
	if !hmac.Equal(decoded, expected) {
		return errors.New("运维请求签名无效")
	}
	return nil
}
