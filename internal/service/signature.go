package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// SignatureService produces the code embedded in an assignment's QR/barcode
// stamp. This is NOT a legally-binding digital signature (no PSrE
// certificate, no non-repudiation) — it is a tamper-evident code proving the
// scanned assignment id was genuinely issued by this engine, so a printed
// approval document can be checked online. Verify only checks the code
// itself; callers must still look up the assignment's live status to see
// what was actually decided.
type SignatureService struct {
	secret []byte
}

func NewSignatureService(secret string) *SignatureService {
	return &SignatureService{secret: []byte(secret)}
}

func (s *SignatureService) Sign(assignmentID string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(assignmentID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *SignatureService) Verify(assignmentID, sig string) bool {
	expected := s.Sign(assignmentID)
	return hmac.Equal([]byte(expected), []byte(sig))
}
