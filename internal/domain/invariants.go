package domain

import "fmt"

func ValidateAggregate(b *PlateBatch) error {
	if b == nil {
		return NewError(CodeIntegrity, "批次聚合为空")
	}
	if err := ValidateIdentifier("batch.id", b.ID); err != nil {
		return integrity(err)
	}
	if b.Revision < 1 {
		return NewError(CodeIntegrity, "批次 %s 的修订号无效", b.ID)
	}
	if b.CatalogStart <= 0 || b.CatalogEnd < b.CatalogStart {
		return NewError(CodeIntegrity, "批次 %s 的目录范围无效", b.ID)
	}
	if _, err := ResolvePolicy(b.QualityPolicyVersion); err != nil {
		return integrity(err)
	}
	if !validState(b.State) {
		return NewError(CodeIntegrity, "批次 %s 包含未知状态 %s", b.ID, b.State)
	}
	if b.State == StateSealed && b.SealedAt == nil {
		return NewError(CodeIntegrity, "封存批次 %s 缺少 sealed_at", b.ID)
	}
	if b.State != StateSealed && b.SealedAt != nil {
		return NewError(CodeIntegrity, "未封存批次 %s 意外包含 sealed_at", b.ID)
	}
	if err := validateCalibrationInvariant(b); err != nil {
		return err
	}
	scans, err := validateScanInvariants(b)
	if err != nil {
		return err
	}
	if err = validateIssueInvariants(b, scans); err != nil {
		return err
	}
	if err = validateConclusionInvariants(b, scans); err != nil {
		return err
	}
	if err = validateStateInvariant(b); err != nil {
		return err
	}
	return nil
}

func integrity(err error) error {
	return NewError(CodeIntegrity, "聚合不变量校验失败: %v", err)
}

func validState(state BatchState) bool {
	switch state {
	case StateDraft, StateCapturing, StateQualityReview, StateRemediation, StatePeerReview, StatePendingArchive, StateSealed:
		return true
	default:
		return false
	}
}

func IsValidBatchState(state BatchState) bool { return validState(state) }

func validateCalibrationInvariant(b *PlateBatch) error {
	if len(b.Calibrations) == 0 {
		if b.State != StateDraft {
			return NewError(CodeIntegrity, "批次 %s 未标定却处于 %s", b.ID, b.State)
		}
		return nil
	}
	ids := map[string]bool{}
	last := b.Calibrations[len(b.Calibrations)-1]
	for _, c := range b.Calibrations {
		if c.BatchID != b.ID {
			return NewError(CodeIntegrity, "批次 %s 的标定 batch_id 不匹配", b.ID)
		}
		if ids[c.ID] {
			return NewError(CodeIntegrity, "批次 %s 的标定会话 ID 重复", b.ID)
		}
		ids[c.ID] = true
		passed := c.ResolutionDPI >= MinResolutionDPI && c.GrayResponseError <= MaxGrayResponseError && c.GrayResponseError >= 0 && c.GeometryErrorPercent <= MaxGeometryErrorPercent && c.GeometryErrorPercent >= 0
		expected := "failed"
		if passed {
			expected = "passed"
		}
		if c.Result != expected {
			return NewError(CodeIntegrity, "批次 %s 的标定结论与指标不一致", b.ID)
		}
	}
	if b.Calibration == nil || b.Calibration.ID != last.ID {
		return NewError(CodeIntegrity, "批次 %s 的当前标定指针无效", b.ID)
	}
	if last.Result != "passed" && b.State != StateDraft {
		return NewError(CodeIntegrity, "批次 %s 以失败标定进入业务流程", b.ID)
	}
	return nil
}

func validateScanInvariants(b *PlateBatch) (map[string]PlateScan, error) {
	byID := make(map[string]PlateScan, len(b.Scans))
	checksums := map[string]string{}
	for _, scan := range b.Scans {
		if scan.BatchID != b.ID {
			return nil, NewError(CodeIntegrity, "扫描 %s 的 batch_id 不匹配", scan.ID)
		}
		if _, ok := byID[scan.ID]; ok {
			return nil, NewError(CodeIntegrity, "扫描 ID %s 重复", scan.ID)
		}
		if scan.CatalogNumber < b.CatalogStart || scan.CatalogNumber > b.CatalogEnd {
			return nil, NewError(CodeIntegrity, "扫描 %s 超出目录范围", scan.ID)
		}
		if owner, ok := checksums[scan.ContentChecksum]; ok && owner != scan.ID {
			return nil, NewError(CodeIntegrity, "扫描摘要 %s 重复", scan.ContentChecksum)
		}
		checksums[scan.ContentChecksum] = scan.ID
		byID[scan.ID] = scan
	}
	superseded := map[string]bool{}
	activeCatalogs := map[int]string{}
	for _, scan := range b.Scans {
		if scan.Version < 1 {
			return nil, NewError(CodeIntegrity, "扫描 %s 的版本无效", scan.ID)
		}
		if scan.Version == 1 && scan.SupersedesScanID != "" {
			return nil, NewError(CodeIntegrity, "首版扫描 %s 包含替代引用", scan.ID)
		}
		if scan.Version > 1 {
			previous, ok := byID[scan.SupersedesScanID]
			if !ok || previous.Version+1 != scan.Version {
				return nil, NewError(CodeIntegrity, "扫描 %s 未直接连续替代前一版本", scan.ID)
			}
			if superseded[previous.ID] {
				return nil, NewError(CodeIntegrity, "扫描 %s 被多个版本替代", previous.ID)
			}
			superseded[previous.ID] = true
		}
	}
	for _, scan := range b.Scans {
		if superseded[scan.ID] {
			continue
		}
		if other := activeCatalogs[scan.CatalogNumber]; other != "" {
			return nil, NewError(CodeIntegrity, "目录号 %d 存在多个活动扫描", scan.CatalogNumber)
		}
		activeCatalogs[scan.CatalogNumber] = scan.ID
	}
	return byID, nil
}

func validateIssueInvariants(b *PlateBatch, scans map[string]PlateScan) error {
	ids := map[string]bool{}
	for _, issue := range b.Issues {
		if issue.BatchID != b.ID {
			return NewError(CodeIntegrity, "质量问题 %s 的 batch_id 不匹配", issue.ID)
		}
		if ids[issue.ID] {
			return NewError(CodeIntegrity, "质量问题 ID %s 重复", issue.ID)
		}
		ids[issue.ID] = true
		if _, ok := scans[issue.ScanID]; !ok {
			return NewError(CodeIntegrity, "质量问题 %s 引用了不存在的扫描", issue.ID)
		}
		if issue.Status != "open" && issue.Status != "closed" {
			return NewError(CodeIntegrity, "质量问题 %s 状态无效", issue.ID)
		}
		if issue.Status == "closed" {
			if issue.ResolvedAt == nil || issue.ResolvedBy == "" || issue.ResolutionKind == "" {
				return NewError(CodeIntegrity, "已关闭问题 %s 缺少裁决证据", issue.ID)
			}
			if issue.ResolutionKind == "rescanned" {
				replacement, ok := scans[issue.ReplacementScanID]
				if !ok || replacement.SupersedesScanID != issue.ScanID {
					return NewError(CodeIntegrity, "问题 %s 的替代谱系无效", issue.ID)
				}
			}
			if len(issue.ResolutionHistory) > 0 && issue.ResolutionHistory[len(issue.ResolutionHistory)-1].RevokedAt != nil {
				return NewError(CodeIntegrity, "已关闭问题 %s 缺少当前有效裁决历史", issue.ID)
			}
		} else if len(issue.ResolutionHistory) > 0 && issue.ResolutionHistory[len(issue.ResolutionHistory)-1].RevokedAt == nil {
			return NewError(CodeIntegrity, "开放问题 %s 仍包含有效裁决", issue.ID)
		}
	}
	return nil
}

func validateConclusionInvariants(b *PlateBatch, scans map[string]PlateScan) error {
	seen := map[int]bool{}
	for _, conclusion := range b.QualityConclusions {
		scan, ok := scans[conclusion.ScanID]
		if !ok || scan.CatalogNumber != conclusion.CatalogNumber {
			return NewError(CodeIntegrity, "目录号 %d 的质量结论引用无效", conclusion.CatalogNumber)
		}
		if seen[conclusion.CatalogNumber] {
			return NewError(CodeIntegrity, "目录号 %d 存在重复活动质量结论", conclusion.CatalogNumber)
		}
		seen[conclusion.CatalogNumber] = true
		passed := true
		for _, metric := range conclusion.Metrics {
			if !metric.Passed {
				passed = false
			}
		}
		if conclusion.Passed != passed {
			return NewError(CodeIntegrity, "目录号 %d 的质量结论汇总不一致", conclusion.CatalogNumber)
		}
	}
	return nil
}

func validateStateInvariant(b *PlateBatch) error {
	open := 0
	for _, issue := range b.Issues {
		if issue.Status == "open" {
			open++
		}
	}
	switch b.State {
	case StateQualityReview:
		if open != 0 || len(b.QualityConclusions) == 0 {
			return NewError(CodeIntegrity, "质量复核状态的批次缺少合格结论或仍有开放问题")
		}
	case StatePeerReview:
		if open != 0 {
			return NewError(CodeIntegrity, "同行抽验状态仍有 %d 个开放问题", open)
		}
	case StatePendingArchive, StateSealed:
		if open != 0 || len(b.PeerReviews) == 0 || !b.PeerReviews[len(b.PeerReviews)-1].Passed {
			return NewError(CodeIntegrity, "批次 %s 缺少通过的最终同行抽验", b.ID)
		}
	}
	return nil
}

func MissingCatalogs(b *PlateBatch) []int {
	active := map[int]bool{}
	for _, scan := range b.ActiveScans() {
		active[scan.CatalogNumber] = true
	}
	missing := []int{}
	for catalog := b.CatalogStart; catalog <= b.CatalogEnd; catalog++ {
		if !active[catalog] {
			missing = append(missing, catalog)
		}
	}
	return missing
}

func OpenIssueCount(b *PlateBatch) int {
	count := 0
	for _, issue := range b.Issues {
		if issue.Status == "open" {
			count++
		}
	}
	return count
}

func AggregateDescription(b *PlateBatch) string {
	return fmt.Sprintf("批次 %s（%d-%d），状态 %s，修订 %d", b.ID, b.CatalogStart, b.CatalogEnd, b.State, b.Revision)
}
