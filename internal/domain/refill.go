package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (b *PlateBatch) CalculateQuality() ([]ScanQualityConclusion, QualitySummary, error) {
	if err := ValidateScanLineage(b); err != nil {
		return nil, QualitySummary{}, err
	}
	policy, err := ResolvePolicy(b.QualityPolicyVersion)
	if err != nil {
		return nil, QualitySummary{}, err
	}
	active := b.ActiveScans()
	conclusions := make([]ScanQualityConclusion, 0, len(active))
	summary := QualitySummary{Total: len(active)}
	for _, scan := range active {
		conclusion := policy.Evaluate(scan)
		sort.Slice(conclusion.Metrics, func(i, j int) bool { return conclusion.Metrics[i].RuleCode < conclusion.Metrics[j].RuleCode })
		conclusions = append(conclusions, conclusion)
		if conclusion.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}
		for _, metric := range conclusion.Metrics {
			if !metric.Passed {
				summary.IssueCount++
			}
		}
	}
	sort.Slice(conclusions, func(i, j int) bool { return conclusions[i].CatalogNumber < conclusions[j].CatalogNumber })
	return conclusions, summary, nil
}

func (b *PlateBatch) CorrectScan(targetID string, replacement PlateScan, reason string) ([]FieldChange, error) {
	if b.State != StateCapturing {
		return nil, NewError(CodeInvalidState, "只有 capturing 状态可更正扫描登记")
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return nil, NewError(CodeValidation, "更正理由不能为空且不能超过 2000 字节")
	}
	if err := ValidateIdentifier("scan_id", targetID); err != nil {
		return nil, err
	}
	var target *PlateScan
	for i := range b.Scans {
		if b.Scans[i].ID == targetID {
			target = &b.Scans[i]
			break
		}
	}
	if target == nil {
		return nil, NewError(CodeNotFound, "被更正扫描不存在")
	}
	active := false
	for _, scan := range b.ActiveScans() {
		if scan.ID == targetID {
			active = true
		}
	}
	if !active {
		return nil, NewError(CodeConflict, "目标扫描已被更新版本替代")
	}
	if replacement.CatalogNumber < b.CatalogStart || replacement.CatalogNumber > b.CatalogEnd {
		return nil, NewError(CodeValidation, "目录号 %d 不在批次范围内", replacement.CatalogNumber)
	}
	if err := ValidateIdentifier("scan_id", replacement.ID); err != nil {
		return nil, err
	}
	if err := ValidateChecksum(replacement.ContentChecksum); err != nil {
		return nil, err
	}
	if err := ValidatePrincipal("captured_by", replacement.CapturedBy); err != nil {
		return nil, err
	}
	if err := ValidateUnitScore("exposure_score", replacement.ExposureScore); err != nil {
		return nil, err
	}
	if err := ValidateUnitScore("focus_score", replacement.FocusScore); err != nil {
		return nil, err
	}
	if replacement.PixelWidth <= 0 || replacement.PixelHeight <= 0 || replacement.BitDepth <= 0 {
		return nil, NewError(CodeValidation, "扫描尺寸和位深必须为正整数")
	}
	for _, scan := range b.Scans {
		if scan.ContentChecksum == replacement.ContentChecksum {
			return nil, NewError(CodeDuplicate, "扫描摘要已用于批次中的其他版本")
		}
		if scan.ID == replacement.ID {
			return nil, NewError(CodeDuplicate, "扫描 ID 已存在")
		}
	}
	for _, scan := range b.ActiveScans() {
		if scan.ID != targetID && scan.CatalogNumber == replacement.CatalogNumber {
			return nil, NewError(CodeDuplicate, "更正后的目录号已有活动扫描")
		}
	}
	replacement.BatchID = b.ID
	replacement.Version = target.Version + 1
	replacement.SupersedesScanID = target.ID
	replacement.CapturedAt = replacement.CapturedAt.UTC()
	changes := scanChanges(*target, replacement)
	if len(changes) == 0 {
		return nil, NewError(CodeValidation, "更正版本未包含扫描元数据变化")
	}
	b.Scans = append(b.Scans, replacement)
	b.Revision++
	return changes, nil
}

func scanChanges(old, next PlateScan) []FieldChange {
	out := []FieldChange{}
	add := func(field string, a, b any) {
		if fmt.Sprint(a) != fmt.Sprint(b) {
			out = append(out, FieldChange{Field: field, Old: a, New: b})
		}
	}
	add("catalog_number", old.CatalogNumber, next.CatalogNumber)
	add("content_checksum", old.ContentChecksum, next.ContentChecksum)
	add("pixel_width", old.PixelWidth, next.PixelWidth)
	add("pixel_height", old.PixelHeight, next.PixelHeight)
	add("bit_depth", old.BitDepth, next.BitDepth)
	add("exposure_score", old.ExposureScore, next.ExposureScore)
	add("focus_score", old.FocusScore, next.FocusScore)
	return out
}

func (b *PlateBatch) RevokeIssueResolution(issueID, reason, actor string, now time.Time) (IssueResolutionRecord, error) {
	if b.State != StateRemediation {
		return IssueResolutionRecord{}, NewError(CodeInvalidState, "只有 remediation 状态可撤销质量裁决")
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return IssueResolutionRecord{}, NewError(CodeValidation, "撤销理由不能为空且不能超过 2000 字节")
	}
	if err := ValidatePrincipal("actor", actor); err != nil {
		return IssueResolutionRecord{}, err
	}
	var issue *QualityIssue
	for i := range b.Issues {
		if b.Issues[i].ID == issueID {
			issue = &b.Issues[i]
			break
		}
	}
	if issue == nil {
		return IssueResolutionRecord{}, NewError(CodeNotFound, "质量问题不存在")
	}
	if issue.Status != "closed" {
		return IssueResolutionRecord{}, NewError(CodeInvalidState, "开放问题没有可撤销的当前裁决")
	}
	if len(issue.ResolutionHistory) == 0 {
		if issue.ResolvedAt == nil {
			return IssueResolutionRecord{}, NewError(CodeIntegrity, "已关闭问题缺少裁决时间")
		}
		issue.ResolutionHistory = append(issue.ResolutionHistory, IssueResolutionRecord{ResolutionKind: issue.ResolutionKind, ResolutionNote: issue.ResolutionNote, ReplacementScanID: issue.ReplacementScanID, ResolvedBy: issue.ResolvedBy, ResolvedAt: issue.ResolvedAt.UTC()})
	}
	if issue.ResolutionHistory[len(issue.ResolutionHistory)-1].RevokedAt != nil {
		return IssueResolutionRecord{}, NewError(CodeConflict, "当前有效裁决已被撤销")
	}
	t := now.UTC()
	history := &issue.ResolutionHistory[len(issue.ResolutionHistory)-1]
	history.RevokedReason, history.RevokedBy, history.RevokedAt = reason, actor, &t
	revoked := *history
	issue.Status, issue.ResolutionKind, issue.ResolutionNote, issue.ReplacementScanID, issue.ResolvedBy, issue.ResolvedAt = "open", "", "", "", "", nil
	b.Revision++
	return revoked, nil
}

type ManifestActualEntry struct {
	CatalogNumber    int    `json:"catalog_number"`
	ScanID           string `json:"scan_id,omitempty"`
	Version          int    `json:"version,omitempty"`
	ContentChecksum  string `json:"content_checksum"`
	PixelWidth       int    `json:"pixel_width"`
	PixelHeight      int    `json:"pixel_height"`
	BitDepth         int    `json:"bit_depth"`
	SupersedesScanID string `json:"supersedes_scan_id,omitempty"`
}
type ManifestDifference struct {
	CatalogNumber int                  `json:"catalog_number"`
	Expected      *ManifestEntry       `json:"expected,omitempty"`
	Actual        *ManifestActualEntry `json:"actual,omitempty"`
	Fields        []string             `json:"fields,omitempty"`
}
type ManifestReconciliation struct {
	Matched               bool                 `json:"matched"`
	ManifestHash          string               `json:"manifest_hash"`
	Missing               []ManifestDifference `json:"missing"`
	Unexpected            []ManifestDifference `json:"unexpected"`
	ChecksumMismatch      []ManifestDifference `json:"checksum_mismatch"`
	MetadataMismatch      []ManifestDifference `json:"metadata_mismatch"`
	MissingCount          int                  `json:"missing_count"`
	UnexpectedCount       int                  `json:"unexpected_count"`
	ChecksumMismatchCount int                  `json:"checksum_mismatch_count"`
	MetadataMismatchCount int                  `json:"metadata_mismatch_count"`
}

func ReconcileManifest(m ArchiveManifest, actual []ManifestActualEntry) (ManifestReconciliation, error) {
	out := ManifestReconciliation{ManifestHash: m.ManifestHash, Missing: []ManifestDifference{}, Unexpected: []ManifestDifference{}, ChecksumMismatch: []ManifestDifference{}, MetadataMismatch: []ManifestDifference{}}
	want := map[int]ManifestEntry{}
	got := map[int]ManifestActualEntry{}
	for _, e := range m.Entries {
		want[e.CatalogNumber] = e
	}
	for _, e := range actual {
		if e.CatalogNumber <= 0 || e.PixelWidth <= 0 || e.PixelHeight <= 0 || e.BitDepth <= 0 {
			return out, NewError(CodeValidation, "成果条目的目录号、尺寸和位深必须为正整数")
		}
		if err := ValidateChecksum(e.ContentChecksum); err != nil {
			return out, err
		}
		if _, exists := got[e.CatalogNumber]; exists {
			return out, NewError(CodeDuplicate, "核验载荷包含重复目录号 %d", e.CatalogNumber)
		}
		got[e.CatalogNumber] = e
	}
	for catalog, e := range want {
		a, ok := got[catalog]
		if !ok {
			x := e
			out.Missing = append(out.Missing, ManifestDifference{CatalogNumber: catalog, Expected: &x})
			continue
		}
		if e.ContentChecksum != a.ContentChecksum {
			x, y := e, a
			out.ChecksumMismatch = append(out.ChecksumMismatch, ManifestDifference{CatalogNumber: catalog, Expected: &x, Actual: &y, Fields: []string{"content_checksum"}})
			continue
		}
		fields := []string{}
		if e.PixelWidth != a.PixelWidth {
			fields = append(fields, "pixel_width")
		}
		if e.PixelHeight != a.PixelHeight {
			fields = append(fields, "pixel_height")
		}
		if e.BitDepth != a.BitDepth {
			fields = append(fields, "bit_depth")
		}
		if len(fields) > 0 {
			x, y := e, a
			out.MetadataMismatch = append(out.MetadataMismatch, ManifestDifference{CatalogNumber: catalog, Expected: &x, Actual: &y, Fields: fields})
		}
	}
	for catalog, a := range got {
		if _, ok := want[catalog]; !ok {
			y := a
			out.Unexpected = append(out.Unexpected, ManifestDifference{CatalogNumber: catalog, Actual: &y})
		}
	}
	sortDiff := func(xs []ManifestDifference) {
		sort.Slice(xs, func(i, j int) bool { return xs[i].CatalogNumber < xs[j].CatalogNumber })
	}
	sortDiff(out.Missing)
	sortDiff(out.Unexpected)
	sortDiff(out.ChecksumMismatch)
	sortDiff(out.MetadataMismatch)
	out.MissingCount = len(out.Missing)
	out.UnexpectedCount = len(out.Unexpected)
	out.ChecksumMismatchCount = len(out.ChecksumMismatch)
	out.MetadataMismatchCount = len(out.MetadataMismatch)
	out.Matched = out.MissingCount+out.UnexpectedCount+out.ChecksumMismatchCount+out.MetadataMismatchCount == 0
	return out, nil
}

func ValidateSealedManifest(b *PlateBatch, m ArchiveManifest) error {
	if b == nil || b.State != StateSealed || m.BatchID != b.ID || m.BatchRevision != b.Revision {
		return NewError(CodeIntegrity, "封存清单与批次身份或修订不一致")
	}
	if !VerifyManifest(m) || m.AuditHeadHash == "" {
		return NewError(CodeIntegrity, "封存清单摘要或审计锚点无效")
	}
	active := b.ActiveScans()
	if len(m.Entries) != len(active) {
		return NewError(CodeIntegrity, "封存清单条目数量与冻结批次不一致")
	}
	for i, entry := range m.Entries {
		scan := active[i]
		if entry.CatalogNumber != scan.CatalogNumber || entry.ScanID != scan.ID || entry.Version != scan.Version || entry.ContentChecksum != scan.ContentChecksum || entry.PixelWidth != scan.PixelWidth || entry.PixelHeight != scan.PixelHeight || entry.BitDepth != scan.BitDepth || entry.SupersedesScanID != scan.SupersedesScanID {
			return NewError(CodeIntegrity, "封存清单目录 %d 与冻结批次不一致", entry.CatalogNumber)
		}
	}
	return nil
}
