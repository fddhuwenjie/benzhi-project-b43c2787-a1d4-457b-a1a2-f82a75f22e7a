package domain

import (
	"sort"

	"time"
)

func (b *PlateBatch) EnsureWritable() error {
	if b.State == StateSealed {
		return NewError(CodeSealed, "批次已封存，禁止修改")
	}
	return nil
}

func (b *PlateBatch) ApplyCalibration(c CalibrationSession) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.State != StateDraft {
		return NewError(CodeInvalidState, "只有草稿批次可提交标定")
	}
	if err := ValidateIdentifier("calibration_id", c.ID); err != nil {
		return err
	}
	for _, existing := range b.Calibrations {
		if existing.ID == c.ID {
			return NewError(CodeDuplicate, "标定会话 ID 已存在")
		}
	}
	if err := ValidatePrincipal("performed_by", c.PerformedBy); err != nil {
		return err
	}
	if c.ResolutionDPI < 1 || c.ResolutionDPI > 20000 {
		return NewError(CodeValidation, "resolution_dpi 必须在 1 到 20000 之间")
	}
	if err := ValidateMeasurement("gray_response_error", c.GrayResponseError, 0, 1); err != nil {
		return err
	}
	if err := ValidateMeasurement("geometry_error_percent", c.GeometryErrorPercent, 0, 100); err != nil {
		return err
	}
	c.BatchID = b.ID
	c.PerformedAt = c.PerformedAt.UTC()
	passed := c.ResolutionDPI >= MinResolutionDPI && c.GrayResponseError <= MaxGrayResponseError && c.GrayResponseError >= 0 && c.GeometryErrorPercent <= MaxGeometryErrorPercent && c.GeometryErrorPercent >= 0
	if passed {
		c.Result = "passed"
	} else {
		c.Result = "failed"
	}
	b.Calibrations = append(b.Calibrations, c)
	sort.Slice(b.Calibrations, func(i, j int) bool {
		if b.Calibrations[i].PerformedAt.Equal(b.Calibrations[j].PerformedAt) {
			return b.Calibrations[i].ID < b.Calibrations[j].ID
		}
		return b.Calibrations[i].PerformedAt.Before(b.Calibrations[j].PerformedAt)
	})
	b.Calibration = &b.Calibrations[len(b.Calibrations)-1]
	if b.Calibration.Result == "passed" {
		b.State = StateCapturing
	} else {
		b.State = StateDraft
	}
	b.Revision++
	return nil
}

func (b *PlateBatch) ResolveIssues(items []IssueResolution, actor string, now time.Time) error {
	if len(items) < 1 || len(items) > 100 {
		return NewError(CodeValidation, "裁决项数量必须在 1 到 100 之间")
	}
	seen := map[string]bool{}
	copyBatch := *b
	copyBatch.Issues = append([]QualityIssue(nil), b.Issues...)
	for i := range copyBatch.Issues {
		copyBatch.Issues[i].ResolutionHistory = append([]IssueResolutionRecord(nil), b.Issues[i].ResolutionHistory...)
	}
	copyBatch.QualityConclusions = append([]ScanQualityConclusion(nil), b.QualityConclusions...)
	for _, item := range items {
		if seen[item.IssueID] {
			return NewError(CodeDuplicate, "批量裁决包含重复 issue_id")
		}
		seen[item.IssueID] = true
		if err := copyBatch.ResolveIssue(item.IssueID, item.ResolutionKind, item.ResolutionNote, item.ReplacementScanID, actor, now); err != nil {
			return err
		}
	}
	copyBatch.Revision = b.Revision + 1
	b.Issues, b.QualityConclusions, b.Revision = copyBatch.Issues, copyBatch.QualityConclusions, copyBatch.Revision
	return nil
}

