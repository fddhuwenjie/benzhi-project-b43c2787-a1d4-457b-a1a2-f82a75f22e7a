package domain

import (
	"sort"
)

func (b *PlateBatch) AddScan(s PlateScan) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.State != StateCapturing && b.State != StateRemediation {
		return NewError(CodeInvalidState, "当前状态不允许登记扫描")
	}
	if s.CatalogNumber < b.CatalogStart || s.CatalogNumber > b.CatalogEnd {
		return NewError(CodeValidation, "目录号 %d 不在批次范围内", s.CatalogNumber)
	}
	if err := ValidateIdentifier("scan_id", s.ID); err != nil {
		return err
	}
	if err := ValidateChecksum(s.ContentChecksum); err != nil {
		return err
	}
	if err := ValidatePrincipal("captured_by", s.CapturedBy); err != nil {
		return err
	}
	if err := ValidateUnitScore("exposure_score", s.ExposureScore); err != nil {
		return err
	}
	if err := ValidateUnitScore("focus_score", s.FocusScore); err != nil {
		return err
	}
	if s.ContentChecksum == "" || s.CapturedBy == "" || s.PixelWidth <= 0 || s.PixelHeight <= 0 || s.BitDepth <= 0 {
		return NewError(CodeValidation, "扫描摘要、尺寸、位深和操作人必须有效")
	}
	var predecessor *PlateScan
	for i := range b.Scans {
		existing := &b.Scans[i]
		if existing.ContentChecksum == s.ContentChecksum {
			return NewError(CodeDuplicate, "扫描摘要已用于批次中的其他版本")
		}
		if existing.ID == s.SupersedesScanID {
			predecessor = existing
		}
	}
	if s.SupersedesScanID == "" {
		for _, active := range b.ActiveScans() {
			if active.CatalogNumber == s.CatalogNumber {
				return NewError(CodeDuplicate, "目录号 %d 已登记，重扫必须指定 supersedes_scan_id", s.CatalogNumber)
			}
		}
		s.Version = 1
	} else {
		if b.State != StateRemediation {
			return NewError(CodeInvalidState, "只有整改状态可登记替代扫描")
		}
		if predecessor == nil || predecessor.CatalogNumber != s.CatalogNumber {
			return NewError(CodeValidation, "替代扫描必须引用同目录号的现有扫描")
		}
		isActive := false
		for _, active := range b.ActiveScans() {
			if active.ID == predecessor.ID {
				isActive = true
			}
		}
		if !isActive {
			return NewError(CodeValidation, "替代扫描必须引用当前活动版本")
		}
		if predecessor.ContentChecksum == s.ContentChecksum {
			return NewError(CodeValidation, "替代扫描摘要必须不同于旧版本")
		}
		s.Version = predecessor.Version + 1
	}
	s.BatchID = b.ID
	s.CapturedAt = s.CapturedAt.UTC()
	b.Scans = append(b.Scans, s)
	b.Revision++
	return nil
}

func (b *PlateBatch) ActiveScans() []PlateScan {
	superseded := make(map[string]bool)
	for _, scan := range b.Scans {
		if scan.SupersedesScanID != "" {
			superseded[scan.SupersedesScanID] = true
		}
	}
	result := make([]PlateScan, 0, len(b.Scans))
	for _, scan := range b.Scans {
		if !superseded[scan.ID] {
			result = append(result, scan)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CatalogNumber < result[j].CatalogNumber })
	return result
}

func (b *PlateBatch) Evaluate(issueID func() string) (QualitySummary, error) {
	if err := b.EnsureWritable(); err != nil {
		return QualitySummary{}, err
	}
	if b.State != StateCapturing {
		return QualitySummary{}, NewError(CodeInvalidState, "只有采集状态可执行质量评估")
	}
	active := b.ActiveScans()
	expected := b.CatalogEnd - b.CatalogStart + 1
	if len(active) != expected {
		return QualitySummary{}, NewError(CodeValidation, "扫描尚未齐套：期望 %d 张，当前 %d 张", expected, len(active))
	}
	conclusions, summary, err := b.CalculateQuality()
	if err != nil {
		return QualitySummary{}, err
	}
	b.Issues = nil
	b.QualityConclusions = conclusions
	for _, conclusion := range conclusions {
		var scan PlateScan
		for _, activeScan := range active {
			if activeScan.ID == conclusion.ScanID {
				scan = activeScan
				break
			}
		}
		for _, metric := range conclusion.Metrics {
			b.addMetricIssue(issueID, scan, metric.RuleCode, metric.ObservedValue, metric.Threshold, !metric.Passed)
		}
	}
	summary.IssueCount = len(b.Issues)
	b.State = StateQualityReview
	if len(b.Issues) > 0 {
		b.State = StateRemediation
	}
	b.Revision++
	return summary, nil
}

func (b *PlateBatch) addMetricIssue(next func() string, scan PlateScan, rule string, observed float64, threshold string, failed bool) {
	if !failed {
		return
	}
	b.Issues = append(b.Issues, QualityIssue{ID: next(), BatchID: b.ID, ScanID: scan.ID, RuleCode: rule, Severity: "error", ObservedValue: observed, Threshold: threshold, Status: "open"})
}

