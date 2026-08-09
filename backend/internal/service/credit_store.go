package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type CreditStorefront struct {
	Products  []CreditTopupProductView `json:"products"`
	ServerNow time.Time                `json:"serverNow"`
}

type CreditTopupProductView struct {
	model.CreditTopupProduct
	EffectivePriceCents int64 `json:"effectivePriceCents"`
	DiscountBasisPoints int   `json:"discountBasisPoints"`
}

type SaveCreditTopupProductRequest struct {
	Code                   string                      `json:"code"`
	Name                   string                      `json:"name"`
	Category               model.CreditProductCategory `json:"category"`
	BaseMicrocredits       int64                       `json:"baseMicrocredits"`
	BonusMicrocredits      int64                       `json:"bonusMicrocredits"`
	PriceCents             int64                       `json:"priceCents"`
	OriginalPriceCents     int64                       `json:"originalPriceCents"`
	RequiredMembershipTier string                      `json:"requiredMembershipTier"`
	WeeklyPurchaseLimit    int                         `json:"weeklyPurchaseLimit"`
	StockLimit             int64                       `json:"stockLimit"`
	SaleEndsAt             *time.Time                  `json:"saleEndsAt"`
	Badge                  string                      `json:"badge"`
	Description            string                      `json:"description"`
	ImageURL               string                      `json:"imageUrl"`
	Enabled                bool                        `json:"enabled"`
	SortOrder              int                         `json:"sortOrder"`
}

type CreateCreditTopupOrderRequest struct {
	ProductID string `json:"productId"`
}

func (s *Service) EnsureDefaultCreditTopupProducts() error {
	saleEnd := time.Date(2026, time.December, 31, 23, 59, 59, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	products := []model.CreditTopupProduct{
		{ID: newID(), Code: "inspiration-sprint", Name: "灵感快冲", Category: model.CreditProductCategorySurprise, BaseMicrocredits: 30000 * CreditScale, PriceCents: 99900, OriginalPriceCents: 199900, Currency: "CNY", RequiredMembershipTier: "max", WeeklyPurchaseLimit: 1, StockLimit: -1, SaleEndsAt: &saleEnd, Badge: "立省 1,000 元", Description: "每 7 天限购 1 次 · 到账积分长期有效", Enabled: true, SortOrder: 10},
		{ID: newID(), Code: "project-sprint", Name: "项目冲刺", Category: model.CreditProductCategorySurprise, BaseMicrocredits: 55000 * CreditScale, PriceCents: 179900, OriginalPriceCents: 359900, Currency: "CNY", RequiredMembershipTier: "max", WeeklyPurchaseLimit: 1, StockLimit: -1, SaleEndsAt: &saleEnd, Badge: "立省 1,800 元", Description: "每 7 天限购 1 次 · 到账积分长期有效", Enabled: true, SortOrder: 20},
		{ID: newID(), Code: "ultra-exclusive", Name: "至尊专属", Category: model.CreditProductCategorySurprise, BaseMicrocredits: 200000 * CreditScale, PriceCents: 699900, OriginalPriceCents: 1299900, Currency: "CNY", RequiredMembershipTier: "ultra", WeeklyPurchaseLimit: 1, StockLimit: 18, SaleEndsAt: &saleEnd, Badge: "立省 6,000 元", Description: "每 7 天限购 1 次 · 到账积分长期有效", Enabled: true, SortOrder: 30},
		{ID: newID(), Code: "general-heavy", Name: "重度创作", Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 90000 * CreditScale, BonusMicrocredits: 90000 * CreditScale, PriceCents: 599900, OriginalPriceCents: 1199900, Currency: "CNY", RequiredMembershipTier: "max", StockLimit: -1, Badge: "首购超级翻倍", Description: "到账积分长期有效", Enabled: true, SortOrder: 80},
		{ID: newID(), Code: "general-carefree", Name: "无忧创作", Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 225000 * CreditScale, BonusMicrocredits: 225000 * CreditScale, PriceCents: 1499900, OriginalPriceCents: 2999900, Currency: "CNY", RequiredMembershipTier: "max", StockLimit: -1, Badge: "首购超级翻倍", Description: "到账积分长期有效", Enabled: true, SortOrder: 90},
		{ID: newID(), Code: "general-light", Name: "轻量尝鲜", Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 7500 * CreditScale, BonusMicrocredits: 3750 * CreditScale, PriceCents: 49900, OriginalPriceCents: 74900, Currency: "CNY", RequiredMembershipTier: "pro", StockLimit: -1, Badge: "首购加赠 50%", Description: "到账积分长期有效", Enabled: true, SortOrder: 100},
		{ID: newID(), Code: "general-daily", Name: "日常创作", Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 15000 * CreditScale, BonusMicrocredits: 7500 * CreditScale, PriceCents: 99900, OriginalPriceCents: 149900, Currency: "CNY", RequiredMembershipTier: "pro", StockLimit: -1, Badge: "首购加赠 50%", Description: "到账积分长期有效", Enabled: true, SortOrder: 110},
		{ID: newID(), Code: "general-project", Name: "项目启动", Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 30000 * CreditScale, BonusMicrocredits: 15000 * CreditScale, PriceCents: 199900, OriginalPriceCents: 299900, Currency: "CNY", RequiredMembershipTier: "max", StockLimit: -1, Badge: "首购加赠 50%", Description: "到账积分长期有效", Enabled: true, SortOrder: 120},
		{ID: newID(), Code: "general-frequent", Name: "高频创作", Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 45000 * CreditScale, BonusMicrocredits: 22500 * CreditScale, PriceCents: 299900, OriginalPriceCents: 449900, Currency: "CNY", RequiredMembershipTier: "max", StockLimit: -1, Badge: "首购加赠 50%", Description: "到账积分长期有效", Enabled: true, SortOrder: 130},
	}
	for index := range products {
		if err := s.repo.CreateCreditTopupProductIfMissing(&products[index]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreditStorefront(user *model.User) (*CreditStorefront, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	products, err := s.repo.CreditTopupProducts(false)
	if err != nil {
		return nil, err
	}
	views := make([]CreditTopupProductView, 0, len(products))
	for index := range products {
		price, discount, priceErr := s.creditTopupPrice(user, &products[index])
		if priceErr != nil {
			return nil, priceErr
		}
		views = append(views, CreditTopupProductView{CreditTopupProduct: products[index], EffectivePriceCents: price, DiscountBasisPoints: discount})
	}
	return &CreditStorefront{Products: views, ServerNow: time.Now().UTC()}, nil
}

func (s *Service) AdminCreditTopupProducts(actor *model.User) ([]model.CreditTopupProduct, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.repo.CreditTopupProducts(true)
}

func (s *Service) SaveCreditTopupProduct(actor *model.User, id string, req SaveCreditTopupProductRequest) (*model.CreditTopupProduct, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	productID := strings.TrimSpace(id)
	if productID != "" {
		if _, err := s.repo.CreditTopupProduct(productID); errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, BadAuthRequest("积分商品不存在")
		} else if err != nil {
			return nil, err
		}
	}
	product := &model.CreditTopupProduct{
		ID: productID, Code: strings.TrimSpace(req.Code), Name: strings.TrimSpace(req.Name), Category: req.Category,
		BaseMicrocredits:  req.BaseMicrocredits,
		BonusMicrocredits: req.BonusMicrocredits, PriceCents: req.PriceCents, OriginalPriceCents: req.OriginalPriceCents,
		Currency: "CNY", RequiredMembershipTier: strings.TrimSpace(req.RequiredMembershipTier),
		WeeklyPurchaseLimit: req.WeeklyPurchaseLimit, StockLimit: req.StockLimit, SaleEndsAt: req.SaleEndsAt, Badge: strings.TrimSpace(req.Badge),
		Description: strings.TrimSpace(req.Description), ImageURL: strings.TrimSpace(req.ImageURL), Enabled: req.Enabled, SortOrder: req.SortOrder,
		UpdatedAt: time.Now(),
	}
	if product.ID == "" {
		product.ID, product.CreatedAt = newID(), product.UpdatedAt
	}
	if err := validateCreditTopupProduct(product); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	audit, err := newAdminAuditEvent(actor, "credit_product.save", "credit_topup_product", product.ID, "保存积分商品", product)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveCreditTopupProduct(product, audit); err != nil {
		return nil, err
	}
	return s.repo.CreditTopupProduct(product.ID)
}

func (s *Service) CreateCreditTopupOrder(user *model.User, req CreateCreditTopupOrderRequest, idempotencyKey string) (*model.CreditTopupOrder, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	productID := strings.TrimSpace(req.ProductID)
	key := strings.TrimSpace(idempotencyKey)
	if productID == "" || key == "" || len(key) > 120 {
		return nil, BadAuthRequest("商品和 Idempotency-Key 均为必填")
	}
	product, err := s.repo.CreditTopupProduct(productID)
	if err != nil {
		return nil, err
	}
	if !product.Enabled {
		return nil, BadAuthRequest("该积分商品已下架")
	}
	if product.SaleEndsAt != nil && !product.SaleEndsAt.After(time.Now()) {
		return nil, BadAuthRequest("该积分商品活动已结束")
	}
	if err := s.requireCreditProductTier(user, product.RequiredMembershipTier); err != nil {
		return nil, err
	}
	effectivePrice, _, err := s.creditTopupPrice(user, product)
	if err != nil {
		return nil, err
	}
	snapshot, err := json.Marshal(product)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(snapshot)
	now := time.Now()
	order := &model.CreditTopupOrder{
		ID: newID(), OrderNumber: "C" + now.Format("20060102150405") + strings.ToUpper(newID()[:6]), UserID: user.ID,
		ProductID: product.ID, BaseMicrocredits: product.BaseMicrocredits, BonusMicrocredits: product.BonusMicrocredits,
		TotalMicrocredits: product.BaseMicrocredits + product.BonusMicrocredits, TotalPriceCents: effectivePrice,
		Currency: product.Currency, Status: model.CreditTopupOrderPending, ProductSnapshotJSON: string(snapshot),
		IdempotencyKey: key, RequestHash: hex.EncodeToString(digest[:]), CreatedAt: now, UpdatedAt: now,
	}
	order, created, err := s.repo.CreateCreditTopupOrder(order)
	if errors.Is(err, repository.ErrCreditProductOutOfStock) {
		return nil, BadAuthRequest("该积分商品已售罄")
	}
	if errors.Is(err, repository.ErrCreditProductWeeklyLimit) {
		return nil, BadAuthRequest("本周购买次数已达上限")
	}
	if errors.Is(err, repository.ErrCreditProductExpired) {
		return nil, BadAuthRequest("该积分商品活动已结束")
	}
	if errors.Is(err, repository.ErrCreditProductChanged) {
		return nil, &AuthError{Status: 409, Message: "商品配置已更新，请重新确认后下单"}
	}
	if err != nil {
		return nil, err
	}
	if !created && order.RequestHash != hex.EncodeToString(digest[:]) {
		return nil, &AuthError{Status: 409, Message: "同一 Idempotency-Key 不能用于不同商品"}
	}
	return order, nil
}

func (s *Service) MyCreditTopupOrders(user *model.User, page int, limit int) ([]model.CreditTopupOrder, int64, error) {
	if user == nil {
		return nil, 0, Unauthorized("请先登录")
	}
	page, limit = normalizeAdminPage(page, limit)
	return s.repo.CreditTopupOrders(user.ID, limit, (page-1)*limit)
}

func (s *Service) CancelCreditTopupOrder(user *model.User, id string) (*model.CreditTopupOrder, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	if err := s.repo.CancelCreditTopupOrder(user.ID, strings.TrimSpace(id), time.Now()); err != nil {
		if errors.Is(err, repository.ErrCreditTopupOrderNotPending) {
			return nil, BadAuthRequest("只有待支付积分订单可以取消")
		}
		return nil, err
	}
	return s.repo.CreditTopupOrderForUser(user.ID, id)
}

func (s *Service) AdminCreditTopupOrders(actor *model.User, page int, limit int) ([]model.CreditTopupOrder, int64, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, 0, err
	}
	page, limit = normalizeAdminPage(page, limit)
	return s.repo.CreditTopupOrders("", limit, (page-1)*limit)
}

func validateCreditTopupProduct(product *model.CreditTopupProduct) error {
	if product.Code == "" || product.Name == "" || len(product.Code) > 64 || len(product.Name) > 120 {
		return errors.New("商品编码和名称无效")
	}
	if product.Category != model.CreditProductCategorySurprise && product.Category != model.CreditProductCategoryGeneral {
		return errors.New("商品分区无效")
	}
	if product.BaseMicrocredits <= 0 || product.BonusMicrocredits < 0 || product.PriceCents <= 0 || product.OriginalPriceCents < product.PriceCents {
		return errors.New("商品积分或价格无效")
	}
	if product.WeeklyPurchaseLimit < 0 || product.StockLimit < -1 {
		return errors.New("商品限购或库存无效")
	}
	return nil
}

func (s *Service) requireCreditProductTier(user *model.User, required string) error {
	if required == "" || required == "origin" {
		return nil
	}
	entitlement, err := s.MembershipEntitlement(user)
	if err != nil {
		return err
	}
	ranks := map[string]int{"origin": 0, "pro": 1, "max": 2, "ultra": 3}
	current, currentOK := ranks[entitlement.Tier]
	needed, requiredOK := ranks[required]
	if !currentOK || !requiredOK {
		return fmt.Errorf("积分商品会员等级配置无效")
	}
	if !entitlement.IsActiveMember || current < needed {
		return BadAuthRequest("当前会员等级不满足该积分商品购买条件")
	}
	return nil
}

func (s *Service) creditTopupPrice(user *model.User, product *model.CreditTopupProduct) (int64, int, error) {
	entitlement, err := s.MembershipEntitlement(user)
	if err != nil {
		return 0, 0, err
	}
	discount := 10_000
	if entitlement.IsActiveMember && entitlement.TopupDiscountBasis > 0 && entitlement.TopupDiscountBasis <= 10_000 {
		discount = entitlement.TopupDiscountBasis
	}
	price := (product.PriceCents*int64(discount) + 9_999) / 10_000
	if price <= 0 {
		return 0, 0, errors.New("积分商品会员折扣后价格无效")
	}
	return price, discount, nil
}

func (s *Service) creditTopupOrder(id string) (*model.CreditTopupOrder, error) {
	order, err := s.repo.CreditTopupOrder(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return order, err
}
