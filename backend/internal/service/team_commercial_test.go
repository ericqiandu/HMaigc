package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestTeamCreditReservationEnforcesMemberMonthlyLimitAndRefundsAtomically(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "commercial-credit-owner", "credit-owner@example.com")
	member := createTeamTestUser(t, db, "commercial-credit-member", "credit-member@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "积分管控团队"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&model.TeamMember{
		ID: newID(), TeamID: team.ID, UserID: member.ID, Role: model.TeamMemberRoleMember,
		Status: model.TeamMemberStatusActive, MonthlyCreditLimitMicrocredits: 2_000_000,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TeamCreditAccount{
		TeamID: team.ID, AvailableMicrocredits: 10_000_000, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	first := testTeamBillingOrder(team.ID, member.ID, "team-credit-first", 1_500_000)
	if err := svc.repo.ReserveBillingOrder(first); err != nil {
		t.Fatal(err)
	}
	second := testTeamBillingOrder(team.ID, member.ID, "team-credit-second", 600_000)
	if err := svc.repo.ReserveBillingOrder(second); !errors.Is(err, repository.ErrTeamMemberCreditLimit) {
		t.Fatalf("second reservation error = %v, want monthly limit", err)
	}
	if err := svc.repo.RefundBillingOrder(first.ID, "上游调用前失败"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.ReserveBillingOrder(second); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SettleBillingOrder(second.ID, "provider-request-1"); err != nil {
		t.Fatal(err)
	}

	account, err := svc.repo.TeamCreditAccount(team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 9_400_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("unexpected team credit account: %#v", account)
	}
	var ledgerCount int64
	if err := db.Model(&model.TeamCreditLedgerEntry{}).Where("team_id = ?", team.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 4 {
		t.Fatalf("ledger count = %d, want reserve/refund/reserve/consume", ledgerCount)
	}
}

func TestTeamSummaryUsesPurchasedEntitlementSnapshot(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "commercial-snapshot-owner", "snapshot-owner@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "快照权益团队"})
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 8)
	var subscription model.MembershipSubscription
	if err := db.First(&subscription, "team_id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MembershipPlan{}).Where("id = ?", subscription.PlanID).Updates(map[string]interface{}{
		"team_storage_bytes": 1, "shared_assets_enabled": false, "project_permissions_enabled": false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	detail, err := svc.TeamDetail(owner, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.Subscription == nil {
		t.Fatal("team subscription is missing")
	}
	if !detail.Summary.Subscription.SharedAssetsEnabled || !detail.Summary.Subscription.ProjectPermissionsEnabled {
		t.Fatalf("purchased entitlements changed after catalog edit: %#v", detail.Summary.Subscription)
	}
	if detail.Summary.Subscription.TeamStorageBytes != 130*(1<<40) {
		t.Fatalf("storage entitlement = %d, want purchased 130 TiB", detail.Summary.Subscription.TeamStorageBytes)
	}
}

func TestProjectPermissionsUseTeamDefaultsAndClearOverridesOnDetach(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "commercial-project-owner", "project-owner@example.com")
	member := createTeamTestUser(t, db, "commercial-project-member", "project-member@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "项目权限团队"})
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 5)
	now := time.Now()
	if err := db.Create(&model.TeamMember{
		ID: newID(), TeamID: team.ID, UserID: member.ID, Role: model.TeamMemberRoleMember,
		Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := &model.Project{
		ID: newID(), UserID: owner.ID, Name: "商业短剧项目", Type: "short-drama",
		Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	assigned, err := svc.AssignProjectTeam(owner, project.ID, AssignProjectTeamRequest{TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	if assigned.TeamID != team.ID {
		t.Fatalf("assigned team = %q", assigned.TeamID)
	}
	if _, err := svc.repo.ProjectEditableForUser(member.ID, project.ID, now); err != nil {
		t.Fatalf("member should inherit editor access: %v", err)
	}
	if err := svc.UpdateProjectCollaborator(owner, project.ID, member.ID, UpdateProjectCollaboratorRequest{Role: model.ProjectAccessViewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.ProjectEditableForUser(member.ID, project.ID, now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("viewer edit error = %v, want record not found", err)
	}
	if _, err := svc.repo.ProjectReadableForUser(member.ID, project.ID); err != nil {
		t.Fatalf("viewer should retain read access: %v", err)
	}
	if _, err := svc.AssignProjectTeam(owner, project.ID, AssignProjectTeamRequest{TeamID: ""}); err != nil {
		t.Fatal(err)
	}
	var overrideCount int64
	if err := db.Model(&model.ProjectCollaborator{}).Where("project_id = ?", project.ID).Count(&overrideCount).Error; err != nil {
		t.Fatal(err)
	}
	if overrideCount != 0 {
		t.Fatalf("project collaborator overrides were not cleared: %d", overrideCount)
	}
	if _, err := svc.repo.ProjectReadableForUser(member.ID, project.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("detached project remained visible to member: %v", err)
	}
}

func TestTeamSharedAssetIsStoredInTeamScopeAndIsolatedFromNonMembers(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "commercial-asset-owner", "asset-owner@example.com")
	member := createTeamTestUser(t, db, "commercial-asset-member", "asset-member@example.com")
	outsider := createTeamTestUser(t, db, "commercial-asset-outsider", "asset-outsider@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "共享资产团队"})
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 5)
	now := time.Now()
	if err := db.Create(&model.TeamMember{
		ID: newID(), TeamID: team.ID, UserID: member.ID, Role: model.TeamMemberRoleMember,
		Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	header := multipartFileHeader(t, "reference.txt", "team reference")
	resource, err := svc.UploadTeamResource(owner.ID, team.ID, header, "reference", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resource.TeamID != team.ID || resource.UserID != owner.ID || resource.Status != model.ResourceStatusReady {
		t.Fatalf("unexpected team resource: %#v", resource)
	}
	resources, err := svc.TeamResources(member.ID, team.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].ID != resource.ID || resources[0].PublicURL != "" {
		t.Fatalf("unexpected shared resource list: %#v", resources)
	}
	stream, err := svc.OpenTeamResourceRange(member.ID, team.ID, resource.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	content, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "team reference" {
		t.Fatalf("resource content = %q", string(content))
	}
	if _, err := svc.TeamResources(outsider.ID, team.ID, 20); err == nil {
		t.Fatal("non-member unexpectedly read team assets")
	}
}

func TestInvoiceRequestIsUniqueAndRequiresHTTPSForIssuedFile(t *testing.T) {
	svc, db := newMembershipTestService(t)
	user := createTeamTestUser(t, db, "commercial-invoice-user", "invoice-user@example.com")
	admin := createTeamTestUser(t, db, "commercial-invoice-admin", "invoice-admin@example.com")
	admin.Role = model.UserRoleAdmin
	if err := db.Model(admin).Update("role", model.UserRoleAdmin).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.MembershipPlan{
		ID: newID(), Code: "invoice-team-plan", Name: "开票团队版", Tier: "team",
		Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear,
		PriceCents: 199_900, Currency: "CNY", ImageConcurrency: 8, VideoConcurrency: 4,
		InvoicingEnabled: true,
	}
	snapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	order := &model.MembershipOrder{
		ID: newID(), OrderNumber: "M-INVOICE-1", UserID: user.ID, PlanID: plan.ID,
		Seats: 1, UnitPriceCents: plan.PriceCents, TotalPriceCents: plan.PriceCents,
		Currency: plan.Currency, Status: model.MembershipOrderPaid, PlanSnapshotJSON: string(snapshot),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}

	request, err := svc.CreateInvoiceRequest(user, CreateInvoiceRequest{
		MembershipOrderID: order.ID, Title: "弘梦科技有限公司",
		TaxNumber: "91310000TEST", Email: "finance@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateInvoiceRequest(user, CreateInvoiceRequest{
		MembershipOrderID: order.ID, Title: "重复申请", Email: "finance@example.com",
	})
	requireAuthStatus(t, err, http.StatusConflict)

	err = svc.AdminResolveInvoiceRequest(admin, request.ID, ResolveInvoiceRequest{
		Status: model.InvoiceRequestStatusIssued, InvoiceNumber: "INV-001",
		InvoiceURL: "http://unsafe.example.com/invoice.pdf", Note: "测试非 HTTPS",
	})
	requireAuthStatus(t, err, http.StatusBadRequest)
	if err := svc.AdminResolveInvoiceRequest(admin, request.ID, ResolveInvoiceRequest{
		Status: model.InvoiceRequestStatusIssued, InvoiceNumber: "INV-001",
		InvoiceURL: "https://billing.example.com/invoice.pdf", Note: "已由税务系统开具",
	}); err != nil {
		t.Fatal(err)
	}
	var stored model.InvoiceRequest
	if err := db.First(&stored, "id = ?", request.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.InvoiceRequestStatusIssued || stored.InvoiceNumber != "INV-001" || stored.ResolvedBy != admin.ID {
		t.Fatalf("unexpected invoice state: %#v", stored)
	}
}

func testTeamBillingOrder(teamID string, userID string, id string, amount int64) *model.BillingOrder {
	now := time.Now()
	return &model.BillingOrder{
		ID: id, UserID: userID, TeamID: teamID, IdempotencyKey: id,
		ChannelID: "test-channel", Model: "test-model", Capability: "image", Scene: "team-test",
		BillingMode: "per_request", PriceVersion: 1, UnitPriceMicrocredits: amount,
		MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: amount,
		Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now,
	}
}

func multipartFileHeader(t *testing.T, name string, content string) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(int64(body.Len()) + 1024); err != nil {
		t.Fatal(err)
	}
	files := request.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("multipart file count = %d, want 1", len(files))
	}
	return files[0]
}
