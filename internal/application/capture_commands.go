package application

import (
	"context"

	"fmt"
	"sort"

	"astroplate-vault/internal/domain"
	"astroplate-vault/internal/persistence"
)

func (s *Service) Calibrate(ctx context.Context, batchID string, r CalibrationRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "calibrate", "calibration.submitted", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		id := r.CalibrationID
		if id == "" {
			id = r.SessionID
		}
		if r.CalibrationID != "" && r.SessionID != "" && r.CalibrationID != r.SessionID {
			return nil, domain.NewError(domain.CodeValidation, "calibration_id 与 session_id 不能冲突")
		}
		if id == "" {
			id = newID("cal")
		}
		c := domain.CalibrationSession{ID: id, ResolutionDPI: r.ResolutionDPI, GrayResponseError: r.GrayResponseError, GeometryErrorPercent: r.GeometryErrorPercent, PerformedBy: r.Actor, PerformedAt: s.now()}
		err := b.ApplyCalibration(c)
		if err != nil {
			return nil, err
		}
		return map[string]any{"session": b.Calibration, "gates": map[string]bool{"resolution": c.ResolutionDPI >= domain.MinResolutionDPI, "gray_response": c.GrayResponseError <= domain.MaxGrayResponseError, "geometry": c.GeometryErrorPercent <= domain.MaxGeometryErrorPercent}, "failure_reasons": domain.CalibrationFailureReasons(c)}, nil
	})
}

func (s *Service) AddScan(ctx context.Context, batchID string, r ScanRequest) (CommandResult, error) {
	return s.mutate(ctx, batchID, "add_scan", "scan.registered", r.CommandMeta, r, func(b *domain.PlateBatch) (any, error) {
		scan := domain.PlateScan{ID: newID("scan"), CatalogNumber: r.CatalogNumber, ContentChecksum: r.ContentChecksum, PixelWidth: r.PixelWidth, PixelHeight: r.PixelHeight, BitDepth: r.BitDepth, ExposureScore: r.ExposureScore, FocusScore: r.FocusScore, SupersedesScanID: r.SupersedesScanID, CapturedBy: r.Actor, CapturedAt: s.now()}
		if err := b.AddScan(scan); err != nil {
			return nil, err
		}
		return b.Scans[len(b.Scans)-1], nil
	})
}

func (s *Service) BatchAddScans(ctx context.Context, batchID string, r BatchScanRequest) (CommandResult, error) {
	if err := validateMeta(r.CommandMeta, false); err != nil {
		return CommandResult{}, err
	}
	if len(r.Scans) == 0 || len(r.Scans) > 200 {
		return CommandResult{}, domain.NewError(domain.CodeValidation, "scans 数量必须在 1 到 200 之间")
	}
	hash, err := payloadHash(r)
	if err != nil {
		return CommandResult{}, err
	}
	unlock := s.lock(batchID)
	defer unlock()
	var result CommandResult
	err = s.store.WithTx(ctx, func(tx *persistence.Tx) error {
		rec, e := tx.GetIdempotency(ctx, r.RequestID)
		if e != nil {
			return e
		}
		if rec != nil {
			result, e = replayOrConflict(rec, batchID, "batch_add_scan", hash)
			return e
		}
		b, e := tx.LoadBatch(ctx, batchID)
		if e != nil {
			return e
		}
		if b.Revision != r.ExpectedRevision {
			return domain.RevisionConflict(b.Revision)
		}
		stored := b.Revision
		items := append([]BatchScanItem(nil), r.Scans...)
		sort.Slice(items, func(i, j int) bool { return items[i].CatalogNumber < items[j].CatalogNumber })
		for _, it := range items {
			sc := domain.PlateScan{ID: newID("scan"), CatalogNumber: it.CatalogNumber, ContentChecksum: it.ContentChecksum, PixelWidth: it.PixelWidth, PixelHeight: it.PixelHeight, BitDepth: it.BitDepth, ExposureScore: it.ExposureScore, FocusScore: it.FocusScore, SupersedesScanID: it.SupersedesScanID, CapturedBy: r.Actor, CapturedAt: s.now()}
			if e = b.AddScan(sc); e != nil {
				return e
			}
		}
		b.Revision = stored + 1
		if e = tx.SaveBatch(ctx, b, stored); e != nil {
			return e
		}
		catalogs := make([]int, 0, len(items))
		for _, x := range items {
			catalogs = append(catalogs, x.CatalogNumber)
		}
		if e = appendEvent(ctx, tx, b, "batch_scan_registered", r.Actor, map[string]any{"request_id": r.RequestID, "count": len(items), "catalogs": catalogs}, s.now()); e != nil {
			return e
		}
		result = CommandResult{Batch: b}
		return putResult(ctx, tx, r.RequestID, batchID, "batch_add_scan", hash, result)
	})
	return result, err
}

func (s *Service) PrecheckCatalogs(ctx context.Context, batchID string, r CatalogPrecheckRequest) (CatalogPrecheckResponse, error) {
	if err := domain.ValidateIdentifier("request_id", r.RequestID); err != nil {
		return CatalogPrecheckResponse{}, err
	}
	if r.ExpectedRevision < 1 {
		return CatalogPrecheckResponse{}, domain.NewError(domain.CodeValidation, "expected_revision 必须为正整数")
	}
	if len(r.Catalogs) == 0 {
		return CatalogPrecheckResponse{}, domain.NewError(domain.CodeValidation, "目录数组不能为空")
	}
	unlock := s.lock(batchID)
	defer unlock()
	b, err := s.GetBatch(ctx, batchID)
	if err != nil {
		return CatalogPrecheckResponse{}, err
	}
	if b.Revision != r.ExpectedRevision {
		return CatalogPrecheckResponse{}, domain.RevisionConflict(b.Revision)
	}
	if b.State == domain.StateSealed {
		return CatalogPrecheckResponse{}, &domain.Error{Code: domain.CodeSealed, Message: "批次已封存，禁止预检", CurrentRevision: b.Revision}
	}
	if b.State != domain.StateCapturing && b.State != domain.StateRemediation {
		return CatalogPrecheckResponse{}, &domain.Error{Code: domain.CodeInvalidState, Message: "当前状态不允许目录预检", CurrentRevision: b.Revision}
	}
	seen := map[int]bool{}
	sums := map[string]bool{}
	active := map[int]bool{}
	registeredChecksums := map[string]int{}
	for _, x := range b.ActiveScans() {
		active[x.CatalogNumber] = true
	}
	for _, x := range b.Scans {
		registeredChecksums[x.ContentChecksum] = x.CatalogNumber
	}
	out := CatalogPrecheckResponse{BatchID: batchID, BatchRevision: b.Revision, Items: []CatalogPrecheckResult{}}
	for _, x := range r.Catalogs {
		if seen[x.CatalogNumber] || x.ContentChecksum != "" && sums[x.ContentChecksum] {
			return CatalogPrecheckResponse{}, &domain.Error{Code: domain.CodeDuplicate, Message: fmt.Sprintf("预检载荷包含重复目录或校验摘要，冲突目录 %d", x.CatalogNumber), CurrentRevision: b.Revision}
		}
		seen[x.CatalogNumber] = true
		if x.ContentChecksum != "" {
			sums[x.ContentChecksum] = true
			if catalog, ok := registeredChecksums[x.ContentChecksum]; ok && catalog != x.CatalogNumber {
				return CatalogPrecheckResponse{}, &domain.Error{Code: domain.CodeDuplicate, Message: fmt.Sprintf("校验摘要与已登记目录 %d 冲突", catalog), CurrentRevision: b.Revision}
			}
		}
		st := "registerable"
		if x.CatalogNumber < b.CatalogStart || x.CatalogNumber > b.CatalogEnd {
			st = "out_of_range"
			out.OutOfRange = append(out.OutOfRange, x.CatalogNumber)
		} else if active[x.CatalogNumber] {
			st = "already_registered"
			out.AlreadyRegistered = append(out.AlreadyRegistered, x.CatalogNumber)
		}
		out.Items = append(out.Items, CatalogPrecheckResult{x.CatalogNumber, st})
	}
	for c := b.CatalogStart; c <= b.CatalogEnd; c++ {
		if !active[c] && !seen[c] {
			out.MissingCatalogs = append(out.MissingCatalogs, c)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].CatalogNumber < out.Items[j].CatalogNumber })
	sort.Ints(out.AlreadyRegistered)
	sort.Ints(out.OutOfRange)
	return out, nil
}

