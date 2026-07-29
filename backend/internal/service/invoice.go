package service

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type CreateInvoiceRequest struct {
	MembershipOrderID string `json:"membershipOrderId"`
	Title             string `json:"title"`
	TaxNumber         string `json:"taxNumber"`
	Email             string `json:"email"`
}

type ResolveInvoiceRequest struct {
	Status        model.InvoiceRequestStatus `json:"status"`
	InvoiceNumber string                     `json:"invoiceNumber"`
	InvoiceURL    string                     `json:"invoiceUrl"`
	Note          string                     `json:"note"`
}

func (s *Service) MyInvoiceRequests(user *model.User) ([]model.InvoiceRequest, error) {
	return s.repo.InvoiceRequestsForUser(user.ID)
}

func (s *Service) CreateInvoiceRequest(user *model.User, req CreateInvoiceRequest) (*model.InvoiceRequest, error) {
	order, err := s.repo.MembershipOrderForUser(user.ID, strings.TrimSpace(req.MembershipOrderID))
	if err != nil {
		return nil, err
	}
	if order.Status != model.MembershipOrderPaid {
		return nil, BadAuthRequest("只有已支付会员订单可以申请发票")
	}
	plan, err := membershipPlanFromOrderSnapshot(order)
	if err != nil {
		return nil, err
	}
	if !plan.InvoicingEnabled {
		return nil, Forbidden("该订单套餐不包含开票权益")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len([]rune(title)) > 200 {
		return nil, BadAuthRequest("发票抬头不能为空且最多 200 个字符")
	}
	email := strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, BadAuthRequest("接收邮箱格式无效")
	}
	taxNumber := strings.ToUpper(strings.TrimSpace(req.TaxNumber))
	if len(taxNumber) > 80 {
		return nil, BadAuthRequest("纳税人识别号最多 80 个字符")
	}
	now := time.Now()
	request := &model.InvoiceRequest{
		ID: newID(), UserID: user.ID, TeamID: order.TeamID, MembershipOrderID: order.ID,
		Title: title, TaxNumber: taxNumber, Email: email, AmountCents: order.TotalPriceCents,
		Status: model.InvoiceRequestStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateInvoiceRequest(request); err != nil {
		if errors.Is(err, repository.ErrInvoiceRequestExists) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "该会员订单已提交过开票申请"}
		}
		return nil, err
	}
	return request, nil
}

func (s *Service) AdminInvoiceRequests(actor *model.User, status string, page int, limit int) ([]model.InvoiceRequest, int64, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, 0, err
	}
	page, limit = normalizePage(page, limit)
	requestStatus := model.InvoiceRequestStatus(strings.TrimSpace(status))
	if requestStatus != "" && requestStatus != model.InvoiceRequestStatusPending && requestStatus != model.InvoiceRequestStatusIssued && requestStatus != model.InvoiceRequestStatusRejected {
		return nil, 0, BadAuthRequest("开票状态筛选无效")
	}
	return s.repo.AdminInvoiceRequests(requestStatus, limit, (page-1)*limit)
}

func (s *Service) AdminResolveInvoiceRequest(actor *model.User, id string, req ResolveInvoiceRequest) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		return BadAuthRequest("处理备注不能为空")
	}
	invoiceNumber := strings.TrimSpace(req.InvoiceNumber)
	invoiceURL := strings.TrimSpace(req.InvoiceURL)
	switch req.Status {
	case model.InvoiceRequestStatusIssued:
		if invoiceNumber == "" || invoiceURL == "" {
			return BadAuthRequest("标记已开具时必须填写发票号码和发票文件地址")
		}
		parsedURL, parseErr := url.ParseRequestURI(invoiceURL)
		if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
			return BadAuthRequest("发票文件地址必须是有效的 HTTPS 地址")
		}
	case model.InvoiceRequestStatusRejected:
		invoiceNumber, invoiceURL = "", ""
	default:
		return BadAuthRequest("只能将开票申请处理为已开具或已驳回")
	}
	if err := s.repo.ResolveInvoiceRequest(strings.TrimSpace(id), actor.ID, req.Status, invoiceNumber, invoiceURL, note, time.Now()); err != nil {
		if errors.Is(err, repository.ErrInvoiceRequestNotPending) {
			return &AuthError{Status: http.StatusConflict, Message: "开票申请已被处理，不能重复操作"}
		}
		return err
	}
	return s.appendAdminAudit(actor, "invoice_request.resolve", "invoice_request", id, "处理开票申请", req)
}
