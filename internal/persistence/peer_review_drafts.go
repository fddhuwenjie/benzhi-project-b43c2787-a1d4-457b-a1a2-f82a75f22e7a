package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"time"

	"astroplate-vault/internal/domain"
	_ "modernc.org/sqlite"
)

func encodeDraft(d *domain.PeerReviewDraft) ([]byte, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("编码同行抽验草稿: %w", err)
	}
	return raw, nil
}

func scanDraft(scanner interface{ Scan(...any) error }) (*domain.PeerReviewDraft, error) {
	var raw []byte
	var storedRevision int64
	var storedStatus, storedUpdatedAt string
	if err := scanner.Scan(&raw, &storedRevision, &storedStatus, &storedUpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.NewError(domain.CodeNotFound, "同行抽验草稿不存在")
		}
		return nil, err
	}
	var draft domain.PeerReviewDraft
	if err := json.Unmarshal(raw, &draft); err != nil {
		return nil, domain.NewError(domain.CodeIntegrity, "同行抽验草稿无法解码")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, storedUpdatedAt)
	if err != nil {
		return nil, domain.NewError(domain.CodeIntegrity, "同行抽验草稿关系投影时间无效")
	}
	// 关系列用于快速并发检查，加载时以其值刷新文档中的投影视图。
	draft.DraftRevision = storedRevision
	draft.Status = domain.PeerReviewDraftStatus(storedStatus)
	draft.UpdatedAt = updatedAt
	return &draft, nil
}

func (s *Store) LoadPeerReviewDraft(ctx context.Context, batchID, draftID string) (*domain.PeerReviewDraft, error) {
	return scanDraft(s.db.QueryRowContext(ctx, `SELECT document,draft_revision,status,updated_at FROM peer_review_drafts WHERE id=? AND batch_id=?`, draftID, batchID))
}

func (t *Tx) LoadPeerReviewDraft(ctx context.Context, batchID, draftID string) (*domain.PeerReviewDraft, error) {
	return scanDraft(t.tx.QueryRowContext(ctx, `SELECT document,draft_revision,status,updated_at FROM peer_review_drafts WHERE id=? AND batch_id=?`, draftID, batchID))
}

func (t *Tx) FindOpenPeerReviewDraft(ctx context.Context, batchID string, round int, reviewer string) (*domain.PeerReviewDraft, error) {
	var raw []byte
	err := t.tx.QueryRowContext(ctx, `SELECT document FROM peer_review_drafts WHERE batch_id=? AND round_number=? AND reviewer=? AND status='open'`, batchID, round, reviewer).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var draft domain.PeerReviewDraft
	if err = json.Unmarshal(raw, &draft); err != nil {
		return nil, domain.NewError(domain.CodeIntegrity, "同行抽验草稿无法解码")
	}
	return &draft, nil
}

func (t *Tx) InsertPeerReviewDraft(ctx context.Context, d *domain.PeerReviewDraft) error {
	raw, err := encodeDraft(d)
	if err != nil {
		return err
	}
	completed := ""
	if d.CompletedAt != nil {
		completed = d.CompletedAt.Format(time.RFC3339Nano)
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO peer_review_drafts(id,batch_id,round_number,reviewer,base_batch_revision,draft_revision,status,document,created_at,updated_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, d.ID, d.BatchID, d.Round, d.Reviewer, d.BaseBatchRevision, d.DraftRevision, d.Status, raw, d.CreatedAt.Format(time.RFC3339Nano), d.UpdatedAt.Format(time.RFC3339Nano), completed)
	if err != nil {
		return domain.NewError(domain.CodeDuplicate, "本轮该复核员已有开放草稿或草稿 ID 已存在")
	}
	return nil
}

func (t *Tx) SavePeerReviewDraft(ctx context.Context, d *domain.PeerReviewDraft, expectedDraftRevision int64) error {
	raw, err := encodeDraft(d)
	if err != nil {
		return err
	}
	completed := ""
	if d.CompletedAt != nil {
		completed = d.CompletedAt.Format(time.RFC3339Nano)
	}
	result, err := t.tx.ExecContext(ctx, `UPDATE peer_review_drafts SET draft_revision=?,status=?,document=?,updated_at=?,completed_at=? WHERE id=? AND batch_id=? AND draft_revision=?`, d.DraftRevision, d.Status, raw, d.UpdatedAt.Format(time.RFC3339Nano), completed, d.ID, d.BatchID, expectedDraftRevision)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return domain.NewError(domain.CodeConflict, "expected_draft_revision 与当前草稿修订不一致")
	}
	return nil
}
