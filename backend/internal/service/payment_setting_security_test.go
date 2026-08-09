package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	if err := os.Setenv("CANVAS_ENVIRONMENT", "development"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestPaymentRuntimeEnvironmentControlsCheckoutAndNotifyURLs(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		request     PaymentSettingRequest
		wantError   bool
	}{
		{
			name:        "missing environment",
			environment: "",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://checkout.example.com"},
			wantError:   true,
		},
		{
			name:        "unknown environment",
			environment: "staging",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://checkout.example.com"},
			wantError:   true,
		},
		{
			name:        "production HTTP checkout",
			environment: "production",
			request:     PaymentSettingRequest{CheckoutBaseURL: "http://checkout.example.com"},
			wantError:   true,
		},
		{
			name:        "production HTTP notify",
			environment: "production",
			request: PaymentSettingRequest{
				CheckoutBaseURL: "https://checkout.example.com",
				Wechat: PaymentChannelSettingRequest{
					NotifyURL: "http://api.example.com/api/payments/webhooks/wechat",
				},
			},
			wantError: true,
		},
		{
			name:        "development non-loopback HTTP checkout",
			environment: "development",
			request:     PaymentSettingRequest{CheckoutBaseURL: "http://checkout.example.test:3000"},
			wantError:   true,
		},
		{
			name:        "development localhost suffix attack",
			environment: "development",
			request:     PaymentSettingRequest{CheckoutBaseURL: "http://localhost.evil:3000"},
			wantError:   true,
		},
		{
			name:        "development URL userinfo",
			environment: "development",
			request:     PaymentSettingRequest{CheckoutBaseURL: "http://user:password@localhost:3000"},
			wantError:   true,
		},
		{
			name:        "production checkout query",
			environment: "production",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://checkout.example.com?token=unsafe"},
			wantError:   true,
		},
		{
			name:        "production checkout empty query delimiter",
			environment: "production",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://checkout.example.com?"},
			wantError:   true,
		},
		{
			name:        "production checkout path",
			environment: "production",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://checkout.example.com/prefix"},
			wantError:   true,
		},
		{
			name:        "production checkout without hostname",
			environment: "production",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://:443"},
			wantError:   true,
		},
		{
			name:        "production notify fragment",
			environment: "production",
			request: PaymentSettingRequest{Wechat: PaymentChannelSettingRequest{
				NotifyURL: "https://api.example.com/api/payments/webhooks/wechat#fragment",
			}},
			wantError: true,
		},
		{
			name:        "production notify empty fragment delimiter",
			environment: "production",
			request: PaymentSettingRequest{Wechat: PaymentChannelSettingRequest{
				NotifyURL: "https://api.example.com/api/payments/webhooks/wechat#",
			}},
			wantError: true,
		},
		{
			name:        "development localhost HTTP checkout",
			environment: "development",
			request:     PaymentSettingRequest{CheckoutBaseURL: "http://localhost:3000"},
		},
		{
			name:        "development 127 slash 8 HTTP notify",
			environment: "development",
			request: PaymentSettingRequest{
				CheckoutBaseURL: "http://127.42.8.9:3000",
				Alipay: PaymentChannelSettingRequest{
					NotifyURL: "http://127.99.1.2:8080/api/payments/webhooks/alipay",
				},
			},
		},
		{
			name:        "development IPv6 loopback HTTP checkout",
			environment: "development",
			request:     PaymentSettingRequest{CheckoutBaseURL: "http://[::1]:3000"},
		},
		{
			name:        "production HTTPS checkout",
			environment: "production",
			request:     PaymentSettingRequest{CheckoutBaseURL: "https://checkout.example.com"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CANVAS_ENVIRONMENT", test.environment)
			svc, admin, db := newPaymentSettingSecurityService(t)
			_, err := svc.UpdatePaymentSetting(admin, test.request)
			if test.wantError && err == nil {
				t.Fatal("payment setting update succeeded, want explicit runtime URL rejection")
			}
			if !test.wantError && err != nil {
				t.Fatalf("payment setting update failed: %v", err)
			}
			if test.wantError {
				var settingCount int64
				if countErr := db.Model(&model.SystemSetting{}).Where("key = ?", paymentSettingKey).Count(&settingCount).Error; countErr != nil {
					t.Fatalf("count rejected payment settings: %v", countErr)
				}
				var auditCount int64
				if countErr := db.Model(&model.AdminAuditEvent{}).Count(&auditCount).Error; countErr != nil {
					t.Fatalf("count rejected payment audits: %v", countErr)
				}
				if settingCount != 0 || auditCount != 0 {
					t.Fatalf("rejected payment update wrote settings=%d audits=%d", settingCount, auditCount)
				}
			}
		})
	}
}

func TestPaymentRuntimeRejectsPersistedInsecureConfigurationAndPublicOrigins(t *testing.T) {
	tests := []struct {
		name          string
		environment   string
		publicOrigins string
		persisted     *paymentSettingValue
		wantError     bool
	}{
		{
			name:          "missing environment",
			publicOrigins: "http://localhost:3000",
			wantError:     true,
		},
		{
			name:          "unknown environment",
			environment:   "preview",
			publicOrigins: "https://preview.example.com",
			wantError:     true,
		},
		{
			name:        "production origin is required",
			environment: "production",
			wantError:   true,
		},
		{
			name:          "production HTTP checkout persisted",
			environment:   "production",
			publicOrigins: "https://app.example.com",
			persisted: &paymentSettingValue{
				CheckoutBaseURL: "http://checkout.example.com",
			},
			wantError: true,
		},
		{
			name:          "production HTTP notify persisted",
			environment:   "production",
			publicOrigins: "https://app.example.com",
			persisted: &paymentSettingValue{
				CheckoutBaseURL: "https://checkout.example.com",
				Wechat: paymentChannelSettingValue{
					NotifyURL: "http://api.example.com/api/payments/webhooks/wechat",
				},
			},
			wantError: true,
		},
		{
			name:          "production checkout path persisted",
			environment:   "production",
			publicOrigins: "https://app.example.com",
			persisted: &paymentSettingValue{
				CheckoutBaseURL: "https://checkout.example.com/prefix",
			},
			wantError: true,
		},
		{
			name:          "production HTTP public origin",
			environment:   "production",
			publicOrigins: "http://app.example.com",
			wantError:     true,
		},
		{
			name:          "production public origin with path",
			environment:   "production",
			publicOrigins: "https://app.example.com/not-an-origin",
			wantError:     true,
		},
		{
			name:          "production public origin with userinfo",
			environment:   "production",
			publicOrigins: "https://user:password@app.example.com",
			wantError:     true,
		},
		{
			name:          "production public origin with query",
			environment:   "production",
			publicOrigins: "https://app.example.com?tenant=unsafe",
			wantError:     true,
		},
		{
			name:          "production public origin with empty query delimiter",
			environment:   "production",
			publicOrigins: "https://app.example.com?",
			wantError:     true,
		},
		{
			name:          "production public origin with fragment",
			environment:   "production",
			publicOrigins: "https://app.example.com#fragment",
			wantError:     true,
		},
		{
			name:          "production public origin without hostname",
			environment:   "production",
			publicOrigins: "https://:443",
			wantError:     true,
		},
		{
			name:          "development wildcard origin",
			environment:   "development",
			publicOrigins: "*",
			wantError:     true,
		},
		{
			name:          "development non-loopback HTTP public origin",
			environment:   "development",
			publicOrigins: "http://dev.example.test:3000",
			wantError:     true,
		},
		{
			name:          "development localhost suffix public origin attack",
			environment:   "development",
			publicOrigins: "http://localhost.evil:3000",
			wantError:     true,
		},
		{
			name:          "development loopback runtime",
			environment:   "development",
			publicOrigins: "http://localhost:3000,http://127.20.1.1:4173,http://[::1]:3001",
			persisted: &paymentSettingValue{
				CheckoutBaseURL: "http://127.0.0.1:3000",
				Wechat: paymentChannelSettingValue{
					NotifyURL: "http://[::1]:8080/api/payments/webhooks/wechat",
				},
			},
		},
		{
			name:          "production HTTPS runtime",
			environment:   "production",
			publicOrigins: "https://app.example.com,https://admin.example.com:8443",
			persisted: &paymentSettingValue{
				CheckoutBaseURL: "https://checkout.example.com",
				Alipay: paymentChannelSettingValue{
					NotifyURL: "https://api.example.com/api/payments/webhooks/alipay",
				},
			},
		},
		{
			name:          "persisted URL with surrounding whitespace",
			environment:   "production",
			publicOrigins: "https://app.example.com",
			persisted: &paymentSettingValue{
				CheckoutBaseURL: " https://checkout.example.com",
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CANVAS_ENVIRONMENT", test.environment)
			t.Setenv("CANVAS_CORS_ORIGINS", test.publicOrigins)
			svc, _, db := newPaymentSettingSecurityService(t)
			if test.persisted != nil {
				encoded, err := json.Marshal(test.persisted)
				if err != nil {
					t.Fatalf("encode persisted payment setting: %v", err)
				}
				if err := db.Create(&model.SystemSetting{Key: paymentSettingKey, ValueJSON: string(encoded)}).Error; err != nil {
					t.Fatalf("persist payment setting fixture: %v", err)
				}
			}
			err := svc.ValidatePaymentRuntime()
			if test.wantError && err == nil {
				t.Fatal("payment runtime validation succeeded, want explicit rejection")
			}
			if !test.wantError && err != nil {
				t.Fatalf("payment runtime validation failed: %v", err)
			}
		})
	}

	t.Run("persisted malformed JSON", func(t *testing.T) {
		t.Setenv("CANVAS_ENVIRONMENT", "production")
		t.Setenv("CANVAS_CORS_ORIGINS", "https://app.example.com")
		svc, _, db := newPaymentSettingSecurityService(t)
		if err := db.Create(&model.SystemSetting{Key: paymentSettingKey, ValueJSON: "{"}).Error; err != nil {
			t.Fatalf("persist malformed payment setting fixture: %v", err)
		}
		if err := svc.ValidatePaymentRuntime(); err == nil {
			t.Fatal("payment runtime accepted malformed persisted JSON")
		}
	})
}

func TestPaymentRuntimeRejectsRecoveredInsecureCheckoutCapabilities(t *testing.T) {
	const token = "TASK4_RECOVERED_CHECKOUT_TOKEN"
	tests := []struct {
		name        string
		environment string
		checkoutURL string
		wantError   bool
	}{
		{
			name:        "production HTTP recovered URL",
			environment: "production",
			checkoutURL: "http://checkout.example.com/pay/" + token,
			wantError:   true,
		},
		{
			name:        "production pathful recovered URL",
			environment: "production",
			checkoutURL: "https://checkout.example.com/prefix/pay/" + token,
			wantError:   true,
		},
		{
			name:        "development non-loopback HTTP recovered URL",
			environment: "development",
			checkoutURL: "http://checkout.example.test/pay/" + token,
			wantError:   true,
		},
		{
			name:        "recovered URL suffix contains another token",
			environment: "production",
			checkoutURL: "https://checkout.example.com/pay/TASK4_DIFFERENT_TOKEN",
			wantError:   true,
		},
		{
			name:        "production HTTPS recovered URL",
			environment: "production",
			checkoutURL: "https://checkout.example.com/pay/" + token,
		},
		{
			name:        "development loopback HTTP recovered URL",
			environment: "development",
			checkoutURL: "http://127.42.1.9:3000/pay/" + token,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CANVAS_ENVIRONMENT", test.environment)
			svc, _, _ := newPaymentSettingSecurityService(t)
			digest := sha256.Sum256([]byte(token))
			session := &model.PaymentCheckoutSession{
				OrderType: model.PaymentOrderMembership,
				OrderID:   "task4-recovered-order",
				UserID:    "task4-recovered-user",
				TokenHash: hex.EncodeToString(digest[:]),
			}
			ciphertext, err := svc.encryptPaymentCheckoutToken(session, paymentCheckoutCipherPayload{
				Token: token, CheckoutURL: test.checkoutURL,
			})
			if err != nil {
				t.Fatalf("encrypt recovered checkout fixture: %v", err)
			}
			session.TokenCipher = ciphertext
			_, err = svc.recoverPaymentCheckoutPayload(
				session, model.PaymentOrderMembership, session.OrderID, session.UserID,
			)
			if test.wantError && err == nil {
				t.Fatal("insecure recovered checkout capability was accepted")
			}
			if !test.wantError && err != nil {
				t.Fatalf("valid recovered checkout capability was rejected: %v", err)
			}
		})
	}
}

func newPaymentSettingSecurityService(t *testing.T) (*Service, *model.User, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatalf("migrate payment setting test schema: %v", err)
	}
	admin := &model.User{
		ID: "task4-payment-security-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive,
	}
	return New(repository.New(db), t.TempDir()), admin, db
}
