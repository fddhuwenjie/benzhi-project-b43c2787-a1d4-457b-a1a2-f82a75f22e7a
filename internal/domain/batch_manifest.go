package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"time"
)

func (b *PlateBatch) BuildManifest(head, actor string, now time.Time) (ArchiveManifest, error) {
	if err := b.EnsureWritable(); err != nil {
		return ArchiveManifest{}, err
	}
	if b.State != StatePendingArchive {
		return ArchiveManifest{}, NewError(CodeInvalidState, "只有待封存批次可生成清单")
	}
	if actor == "" || head == "" {
		return ArchiveManifest{}, NewError(CodeValidation, "封存人和审计链头不能为空")
	}
	entries := make([]ManifestEntry, 0)
	for _, s := range b.ActiveScans() {
		entries = append(entries, ManifestEntry{CatalogNumber: s.CatalogNumber, ScanID: s.ID, Version: s.Version, ContentChecksum: s.ContentChecksum, PixelWidth: s.PixelWidth, PixelHeight: s.PixelHeight, BitDepth: s.BitDepth, SupersedesScanID: s.SupersedesScanID})
	}
	t := now.UTC()
	b.State = StateSealed
	b.Revision++
	b.SealedAt = &t
	m := ArchiveManifest{BatchID: b.ID, BatchRevision: b.Revision, Entries: entries, AuditHeadHash: head, SealedBy: actor, SealedAt: t}
	hash, err := HashManifest(m)
	if err != nil {
		return ArchiveManifest{}, err
	}
	m.ManifestHash = hash
	return m, nil
}

func HashManifest(m ArchiveManifest) (string, error) {
	m.ManifestHash = ""

	m.SealedBy = ""
	m.SealedAt = time.Time{}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func VerifyManifest(m ArchiveManifest) bool {
	expected, err := HashManifest(m)
	return err == nil && expected == m.ManifestHash
}

