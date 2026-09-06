package service

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"testing"
	"time"
)

func TestProjectDeliveryChecksCurrentCandidateFacts(t *testing.T) {
	for _, test := range []struct {
		name, column, value string
		current             bool
	}{
		{"unchanged", "", "", true},
		{"confirmed", "status", "confirmed", true},
		{"foreign", "project_id", "foreign", false},
		{"edited", "details_json", `{}`, false},
		{"renamed", "name", "changed", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
			seedAgentProjectUnit(t, db)
			call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"candidate-check","operations":[{"operationId":"characters","type":"create_character_candidates","unitId":"runtime-unit","candidates":[{"name":"林夏","aliases":[],"role":"女主角","appearance":"黑色长发","clothing":"红色风衣","physique":"匀称","personality":"坚定","props":"伞","consistencyPrompt":"红色风衣","multiViewPrompt":"正侧背面","voiceLanguage":"普通话","voiceAge":"青年","voiceTimbre":"清亮"}]}]}`)
			seedApprovedAgentCanvasProposal(t, svc, db, call)
			registry, err := newAgentCapabilityRegistry(svc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = registry.Execute(context.Background(), agentRuntimeServiceScope(), call); err != nil {
				t.Fatal(err)
			}
			if test.column != "" {
				if err = db.Model(&model.ProjectAssetCandidate{}).Where("project_id = ?", "runtime-project").Update(test.column, test.value).Error; err != nil {
					t.Fatal(err)
				}
			}
			evidence, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "已写入")
			if err != nil {
				t.Fatal(err)
			}
			if evidence.ProjectCurrent != test.current {
				t.Fatalf("current=%v want %v", evidence.ProjectCurrent, test.current)
			}
		})
	}
}

func TestProjectDeliveryUsesLatestCommittedUnitReplacement(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	first := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"first-shots","operations":[{"operationId":"shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"旧镜头","description":"旧的镜头描述","durationMs":3000}]}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, first)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Execute(context.Background(), agentRuntimeServiceScope(), first); err != nil {
		t.Fatal(err)
	}
	second := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":2,"clientMutationId":"second-shots","operations":[{"operationId":"shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"新镜头","description":"新的镜头描述","durationMs":4000}]}]}`)
	second.ToolCallID = "second-call"
	var record model.AgentToolCall
	if err = db.First(&record, "run_id = ? AND tool_call_id = ?", "runtime-run", first.ToolCallID).Error; err != nil {
		t.Fatal(err)
	}
	proposal, err := agentruntime.NewApprovalProposalForTool(agentRuntimeServiceScope(), second, nil, "runtime-run:second-call:1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	record.ID, record.ToolCallID, record.IdempotencyKey = "second-record", second.ToolCallID, "runtime-run:second-call:1"
	record.InputJSON, record.OutputJSON = string(second.Arguments), `{}`
	record.ApprovalProposalJSON, record.ApprovalProposalHash = string(payload), hash
	record.ApprovalExpiresAt, record.Status = &proposal.ExpiresAt, agentruntime.ToolCallPending
	if err = db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Execute(context.Background(), agentRuntimeServiceScope(), second); err != nil {
		t.Fatal(err)
	}
	evidence, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "已替换")
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.ProjectCurrent {
		t.Fatal("superseded shot IDs incorrectly invalidate latest delivery")
	}
	if _, err := svc.repo.AgentProjectDeliveryFacts("foreign-user", "runtime-project"); err == nil {
		t.Fatal("foreign actor read project facts")
	}
}

func TestProjectDeliveryChecksCurrentShotFacts(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*gorm.DB) error
		current bool
	}{
		{"unchanged", func(db *gorm.DB) error { return nil }, true},
		{"unrelated revision", func(db *gorm.DB) error {
			return db.Model(&model.Project{}).Where("id = ?", "runtime-project").Update("revision", 3).Error
		}, true},
		{"removed", func(db *gorm.DB) error {
			return db.Where("project_id = ?", "runtime-project").Delete(&model.Shot{}).Error
		}, false},
		{"edited", func(db *gorm.DB) error {
			return db.Model(&model.Shot{}).Where("project_id = ?", "runtime-project").Update("description", "changed").Error
		}, false},
		{"foreign", func(db *gorm.DB) error {
			return db.Model(&model.Shot{}).Where("project_id = ?", "runtime-project").Update("project_id", "foreign").Error
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
			seedAgentProjectUnit(t, db)
			call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"delivery-check","operations":[{"operationId":"shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"车站","description":"雨夜车站远景","durationMs":3000}]}]}`)
			seedApprovedAgentCanvasProposal(t, svc, db, call)
			registry, err := newAgentCapabilityRegistry(svc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = registry.Execute(context.Background(), agentRuntimeServiceScope(), call); err != nil {
				t.Fatal(err)
			}
			if err = test.mutate(db); err != nil {
				t.Fatal(err)
			}
			evidence, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "已写入")
			if err != nil {
				t.Fatal(err)
			}
			if evidence.ProjectCurrent != test.current {
				t.Fatalf("current=%v, want %v", evidence.ProjectCurrent, test.current)
			}
			verification := agentruntime.VerifyDelivery(call.ExpectedDelivery, evidence)
			if (verification.Status == agentruntime.VerificationSatisfied) != test.current {
				t.Fatalf("unexpected verdict: %#v", verification)
			}
		})
	}
}
