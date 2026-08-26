package domain

import "fmt"

type QualityPolicy struct {
	Version          string  `json:"version"`
	MinPixelWidth    int     `json:"min_pixel_width"`
	MinPixelHeight   int     `json:"min_pixel_height"`
	RequiredBitDepth int     `json:"required_bit_depth"`
	MinExposureScore float64 `json:"min_exposure_score"`
	MinFocusScore    float64 `json:"min_focus_score"`
}

var policies = map[string]QualityPolicy{
	"v1":          {Version: "v1", MinPixelWidth: MinPixelWidth, MinPixelHeight: MinPixelHeight, RequiredBitDepth: RequiredBitDepth, MinExposureScore: MinExposureScore, MinFocusScore: MinFocusScore},
	"astro-qc-v1": {Version: "astro-qc-v1", MinPixelWidth: MinPixelWidth, MinPixelHeight: MinPixelHeight, RequiredBitDepth: RequiredBitDepth, MinExposureScore: MinExposureScore, MinFocusScore: MinFocusScore},
}

func ResolvePolicy(version string) (QualityPolicy, error) {
	policy, ok := policies[version]
	if !ok {
		return QualityPolicy{}, NewError(CodeValidation, "不支持的质量规则版本 %q", version)
	}
	return policy, nil
}

func (p QualityPolicy) Evaluate(scan PlateScan) ScanQualityConclusion {
	metrics := []MetricConclusion{
		{RuleCode: "pixel_width", ObservedValue: float64(scan.PixelWidth), Threshold: fmt.Sprintf(">=%d", p.MinPixelWidth), Passed: scan.PixelWidth >= p.MinPixelWidth},
		{RuleCode: "pixel_height", ObservedValue: float64(scan.PixelHeight), Threshold: fmt.Sprintf(">=%d", p.MinPixelHeight), Passed: scan.PixelHeight >= p.MinPixelHeight},
		{RuleCode: "bit_depth", ObservedValue: float64(scan.BitDepth), Threshold: fmt.Sprintf("=%d", p.RequiredBitDepth), Passed: scan.BitDepth == p.RequiredBitDepth},
		{RuleCode: "exposure_score", ObservedValue: scan.ExposureScore, Threshold: fmt.Sprintf(">=%.2f", p.MinExposureScore), Passed: scan.ExposureScore >= p.MinExposureScore},
		{RuleCode: "focus_score", ObservedValue: scan.FocusScore, Threshold: fmt.Sprintf(">=%.2f", p.MinFocusScore), Passed: scan.FocusScore >= p.MinFocusScore},
	}
	passed := true
	for _, metric := range metrics {
		if !metric.Passed {
			passed = false
			break
		}
	}
	return ScanQualityConclusion{ScanID: scan.ID, CatalogNumber: scan.CatalogNumber, Passed: passed, Metrics: metrics}
}
