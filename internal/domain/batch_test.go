package domain

import (
	"fmt"
	"testing"
	"time"
)

func testBatch(t *testing.T) *PlateBatch {
	t.Helper()
	b, err := NewBatch("batch-1", "测试批次", 1, 1, "scanner-1", "v1", "operator", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBatchIssueResolutionIsAtomic(t *testing.T) {
	b := testBatch(t)
	b.State = StateRemediation
	b.Issues = []QualityIssue{{ID: "issue-1", BatchID: b.ID, Status: "open"}, {ID: "issue-2", BatchID: b.ID, Status: "open"}}
	revision := b.Revision
	err := b.ResolveIssues([]IssueResolution{{IssueID: "issue-1", ResolutionKind: "accepted", ResolutionNote: "接受"}, {IssueID: "missing", ResolutionKind: "accepted", ResolutionNote: "接受"}}, "reviewer", time.Now())
	if err == nil {
		t.Fatal("包含未知问题的批量裁决被接受")
	}
	if b.Revision != revision || b.Issues[0].Status != "open" || b.Issues[1].Status != "open" {
		t.Fatal("失败的批量裁决污染了聚合")
	}
	err = b.ResolveIssues([]IssueResolution{{IssueID: "issue-2", ResolutionKind: "accepted", ResolutionNote: "接受二"}, {IssueID: "issue-1", ResolutionKind: "accepted", ResolutionNote: "接受一"}}, "reviewer", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if b.Revision != revision+1 || OpenIssueCount(b) != 0 {
		t.Fatal("成功批量裁决未只增加一次修订")
	}
}

func TestPeerEvidenceCreatesIssueOnlyForFailedSample(t *testing.T) {
	b, err := NewBatch("peer-batch", "同行抽验", 1001, 1006, "scanner", "v1", "creator", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b.State = StatePeerReview
	for catalog := b.CatalogStart; catalog <= b.CatalogEnd; catalog++ {
		b.Scans = append(b.Scans, PlateScan{ID: fmt.Sprintf("scan-%d", catalog), BatchID: b.ID, CatalogNumber: catalog, Version: 1, ContentChecksum: fmt.Sprintf("checksum-%d", catalog), CapturedBy: "operator"})
	}
	samples := b.SampleCatalogs()
	inputs := make([]PeerEvidenceInput, 0, len(samples))
	for i, catalog := range samples {
		checksum := fmt.Sprintf("checksum-%d", catalog)
		if i == len(samples)-1 {
			checksum = "checksum-mismatch"
		}
		inputs = append(inputs, PeerEvidenceInput{CatalogNumber: catalog, ObservedChecksum: checksum, DimensionsMatch: true, BitDepthMatch: true})
	}
	if err = b.RecordPeerReviewEvidence("reviewer", inputs, func() string { return "peer-issue" }, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b.State != StateRemediation || len(b.Issues) != 1 || b.Issues[0].ScanID != fmt.Sprintf("scan-%d", samples[len(samples)-1]) {
		t.Fatalf("失败样本问题映射错误: %#v", b.Issues)
	}
}

func calibrate(t *testing.T, b *PlateBatch) {
	t.Helper()
	err := b.ApplyCalibration(CalibrationSession{ID: "cal-1", ResolutionDPI: 3200, GrayResponseError: .01, GeometryErrorPercent: .05, PerformedBy: "operator", PerformedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCalibrationGatePersistsFailure(t *testing.T) {
	b := testBatch(t)
	err := b.ApplyCalibration(CalibrationSession{ID: "bad", ResolutionDPI: 1200, GrayResponseError: .2, GeometryErrorPercent: .5, PerformedBy: "operator", PerformedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if b.State != StateDraft || b.Calibration == nil || b.Calibration.Result != "failed" {
		t.Fatalf("失败标定状态错误: %#v", b)
	}
}

func TestDefectRescanLineageAndSealFreeze(t *testing.T) {
	b := testBatch(t)
	calibrate(t, b)
	bad := PlateScan{ID: "scan-old", CatalogNumber: 1, ContentChecksum: "checksum-old", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .2, CapturedBy: "operator", CapturedAt: time.Now()}
	if err := b.AddScan(bad); err != nil {
		t.Fatal(err)
	}
	summary, err := b.Evaluate(func() string { return "issue-focus" })
	if err != nil {
		t.Fatal(err)
	}
	if summary.IssueCount != 1 || len(b.QualityConclusions) != 1 || b.QualityConclusions[0].Passed {
		t.Fatalf("缺陷结论错误: %#v", summary)
	}
	if err = b.ResolveIssue("issue-focus", "rescanned", "先尝试错误引用", "missing", "operator", time.Now()); err == nil {
		t.Fatal("无效替代谱系被接受")
	}
	replacement := PlateScan{ID: "scan-new", CatalogNumber: 1, ContentChecksum: "checksum-new", PixelWidth: 8000, PixelHeight: 8000, BitDepth: 16, ExposureScore: .9, FocusScore: .95, SupersedesScanID: "scan-old", CapturedBy: "operator", CapturedAt: time.Now()}
	if err = b.AddScan(replacement); err != nil {
		t.Fatal(err)
	}
	if err = b.ResolveIssue("issue-focus", "rescanned", "重新扫描已通过人工检查", "scan-new", "operator", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = b.RequestPeerReview(); err != nil {
		t.Fatal(err)
	}
	sample := b.SampleCatalogs()
	if err = b.RecordPeerReview("operator", sample, true, "", "", time.Now()); err == nil {
		t.Fatal("采集操作人被允许复核")
	}
	if err = b.RecordPeerReview("reviewer", sample, true, "通过", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	m, err := b.BuildManifest("audit-head", "curator", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyManifest(m) || m.Entries[0].ScanID != "scan-new" {
		t.Fatalf("清单错误: %#v", m)
	}
	if err = b.AddScan(replacement); err == nil {
		t.Fatal("封存批次仍可写入")
	}
}

func TestRejectsDuplicateAndOutOfRangeScans(t *testing.T) {
	b, err := NewBatch("b", "批次", 1, 2, "s", "v1", "op", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	calibrate(t, b)
	first := PlateScan{ID: "s1", CatalogNumber: 1, ContentChecksum: "checksum-same", PixelWidth: 1, PixelHeight: 1, BitDepth: 1, CapturedBy: "op", CapturedAt: time.Now()}
	if err = b.AddScan(first); err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ID = "s2"
	if err = b.AddScan(duplicate); err == nil {
		t.Fatal("重复目录号未拒绝")
	}
	outside := first
	outside.ID = "s3"
	outside.CatalogNumber = 3
	outside.ContentChecksum = "checksum-other"
	if err = b.AddScan(outside); err == nil {
		t.Fatal("范围外目录号未拒绝")
	}
}
