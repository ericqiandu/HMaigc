package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
)

var (
	ErrInvoiceRequestExists     = errors.New("invoice request already exists")
	ErrInvoiceRequestNotPending = errors.New("invoice request is not pending")
)

func (r *Repository) CreateInvoiceRequest(request *model.InvoiceRequest) error {
	err := r.db.Create(request).Error
	if err == nil {
		return nil
	}
	var sqliteError sqlite3.Error
	if errors.As(err, &sqliteError) && (sqliteError.ExtendedCode == sqlite3.ErrConstraintUnique || sqliteError.ExtendedCode == sqlite3.ErrConstraintPrimaryKey) {
		return ErrInvoiceRequestExists
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrInvoiceRequestExists
	}
	return err
}

func (r *Repository) InvoiceRequestsForUser(userID string) ([]model.InvoiceRequest, error) {
	var requests []model.InvoiceRequest
	err := r.db.Where("user_id = ?", userID).Order("created_at desc").Find(&requests).Error
	return requests, err
}

func (r *Repository) AdminInvoiceRequests(status model.InvoiceRequestStatus, limit int, offset int) ([]model.InvoiceRequest, int64, error) {
	var requests []model.InvoiceRequest
	var total int64
	query := r.db.Model(&model.InvoiceRequest{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at desc").Limit(limit).Offset(offset).Find(&requests).Error
	return requests, total, err
}

func (r *Repository) ResolveInvoiceRequest(id string, actorUserID string, status model.InvoiceRequestStatus, invoiceNumber string, invoiceURL string, note string, now time.Time) error {
	result := r.db.Model(&model.InvoiceRequest{}).
		Where("id = ? AND status = ?", id, model.InvoiceRequestStatusPending).
		Updates(map[string]interface{}{
			"status": status, "invoice_number": invoiceNumber, "invoice_url": invoiceURL,
			"resolution_note": note, "resolved_by": actorUserID, "resolved_at": &now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInvoiceRequestNotPending
	}
	return nil
}
